import XCTest
@testable import MuseApp

@MainActor
final class MuseumEntryViewModelTests: XCTestCase {
    func test_noMuseumYet_offersCreation() async {
        let service = FakeMuseumService()
        service.fetchResult = .failure(IdentityAPIClientError.server(statusCode: 404, message: "not found"))
        let viewModel = MuseumEntryViewModel(museumService: service, accessToken: "token")

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .needsCreation)
        XCTAssertTrue(viewModel.canOfferCreation)
    }

    func test_museumAlreadyExists_neverOffersCreation() async {
        let service = FakeMuseumService()
        let existing = Museum(id: "m1", styleID: "style_modern", privacy: .private)
        service.fetchResult = .success(existing)
        let viewModel = MuseumEntryViewModel(museumService: service, accessToken: "token")

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .hasMuseum(existing))
        XCTAssertFalse(viewModel.canOfferCreation, "a second Museum must never be offered")
    }

    func test_notFound_isTreatedAsFirstRun_notAsFailure() async {
        let service = FakeMuseumService()
        service.fetchResult = .failure(IdentityAPIClientError.server(statusCode: 404, message: "not found"))
        let viewModel = MuseumEntryViewModel(museumService: service, accessToken: "token")

        await viewModel.load()

        if case .failed = viewModel.state {
            XCTFail("a missing Museum on first run must not be presented as a failure")
        }
    }

    func test_transportFailure_surfacesRetryableFailure_andWithholdsCreation() async {
        let service = FakeMuseumService()
        service.fetchResult = .failure(IdentityAPIClientError.transport)
        let viewModel = MuseumEntryViewModel(museumService: service, accessToken: "token")

        await viewModel.load()

        guard case .failed = viewModel.state else {
            XCTFail("expected .failed, got \(viewModel.state)")
            return
        }
        XCTAssertFalse(viewModel.canOfferCreation)
    }
}

@MainActor
final class StyleSelectionViewModelTests: XCTestCase {
    private func makeViewModel(
        _ service: FakeMuseumService,
        context: StyleSelectionViewModel.Context = .creatingMuseum
    ) -> StyleSelectionViewModel {
        StyleSelectionViewModel(
            context: context,
            museumService: service,
            catalogService: service,
            accessToken: "token"
        )
    }

    func test_listsExactlyTheStylesTheCatalogServes_neverPaddedToFive() async {
        let viewModel = makeViewModel(FakeMuseumService())

        await viewModel.load()

        guard case .ready(let styles) = viewModel.state else {
            XCTFail("expected .ready, got \(viewModel.state)")
            return
        }
        XCTAssertEqual(styles.count, 3)
        XCTAssertEqual(styles.map(\.displayName), ["Modern", "Natural", "Gothic"])
    }

    func test_openStyleGate_isSurfacedAsCopy_withoutNamingTheUndecidedStyles() {
        let notice = StyleSelectionViewModel.openStyleGateNotice

        XCTAssertTrue(notice.contains("not yet defined"), "the gate must be stated, not hidden")
        for invented in ["Industrial", "Minimal", "Classical", "Brutalist", "Art Deco"] {
            XCTAssertFalse(notice.contains(invented), "must not name an undecided style (\(invented))")
        }
    }

    // MARK: - Creation context

    func test_creationContext_createsTheMuseum() async {
        let service = FakeMuseumService()
        let viewModel = makeViewModel(service, context: .creatingMuseum)
        await viewModel.load()

        await viewModel.chooseStyle("style_gothic")

        XCTAssertEqual(service.receivedCreateStyleIDs, ["style_gothic"])
        XCTAssertEqual(service.changeStyleCallCount, 0, "creation must not use the change-style endpoint")
        guard case .applied = viewModel.state else {
            XCTFail("expected .applied, got \(viewModel.state)")
            return
        }
    }

    func test_creationContext_needsNoContentReassurance() {
        let viewModel = makeViewModel(FakeMuseumService(), context: .creatingMuseum)

        XCTAssertFalse(viewModel.requiresContentPreservationReassurance)
        XCTAssertNil(viewModel.currentStyleID)
    }

    // MARK: - Change-style context

    func test_changeContext_changesStyle_ratherThanCreatingASecondMuseum() async {
        let service = FakeMuseumService()
        let viewModel = makeViewModel(service, context: .changingStyle(currentStyleID: "style_modern"))
        await viewModel.load()

        await viewModel.chooseStyle("style_natural")

        XCTAssertEqual(service.changeStyleCallCount, 1)
        XCTAssertTrue(service.receivedCreateStyleIDs.isEmpty, "changing style must never create a Museum")
    }

    func test_changeContext_requiresContentPreservationReassurance() {
        let viewModel = makeViewModel(FakeMuseumService(), context: .changingStyle(currentStyleID: "style_modern"))

        XCTAssertTrue(viewModel.requiresContentPreservationReassurance)
        XCTAssertEqual(viewModel.currentStyleID, "style_modern")
        XCTAssertEqual(
            viewModel.confirmationReassurance,
            "Your Rooms, photos, and content stay exactly as they are.",
            "`02` requires this exact promise when re-skinning a populated Museum"
        )
    }

    func test_changeContext_marksTheAppliedStyleAsCurrent() async {
        let service = FakeMuseumService()
        let viewModel = makeViewModel(service, context: .changingStyle(currentStyleID: "style_modern"))
        await viewModel.load()

        guard case .ready(let styles) = viewModel.state else {
            XCTFail("expected .ready")
            return
        }
        let modern = try? XCTUnwrap(styles.first { $0.id == "style_modern" })
        let gothic = try? XCTUnwrap(styles.first { $0.id == "style_gothic" })
        XCTAssertTrue(viewModel.isCurrentlySelected(modern!))
        XCTAssertFalse(viewModel.isCurrentlySelected(gothic!))
    }

    // MARK: - Failure handling

    func test_failure_preservesTheStyleListForRetry() async {
        let service = FakeMuseumService()
        service.createResult = .failure(IdentityAPIClientError.transport)
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.chooseStyle("style_modern")

        guard case .failed(_, let styles) = viewModel.state else {
            XCTFail("expected .failed, got \(viewModel.state)")
            return
        }
        XCTAssertEqual(styles.count, 3)
    }
}

@MainActor
final class PreviewViewModelTests: XCTestCase {
    private let style = MuseumStyle(
        id: "style_modern",
        displayName: "Modern",
        assetBundle: AssetBundleRef(id: "b1", version: 1)
    )

    private func makeViewModel(
        availability: PreviewAssetAvailability,
        isCurrentlySelected: Bool = false,
        reassurance: String? = nil
    ) -> PreviewViewModel {
        PreviewViewModel(
            subject: style.previewSubject,
            isCurrentlySelected: isCurrentlySelected,
            confirmationReassurance: reassurance,
            assetProvider: StubPreviewAssetProvider(availability: availability)
        )
    }

    func test_withNoAssets_reportsUnavailable_andWithholdsThe3DSurface() async {
        let viewModel = makeViewModel(availability: .unavailable)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .assetsUnavailable)
        XCTAssertFalse(viewModel.shouldPresentImmersiveSurface, "an empty 3D scene must not masquerade as a style preview")
        XCTAssertNotNil(viewModel.statusMessage)
    }

    func test_productionAssetProvider_reportsUnavailable() async {
        let availability = await UnavailablePreviewAssetProvider().availability(for: style.previewSubject)

        XCTAssertEqual(availability, .unavailable)
    }

    func test_progressiveReveal_presentsTheSurfaceOnceGeometryArrives() async {
        let viewModel = makeViewModel(availability: .geometryReady)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .geometryReady)
        XCTAssertTrue(viewModel.shouldPresentImmersiveSurface)
        XCTAssertEqual(viewModel.statusMessage, "Loading materials and lighting…")
    }

    func test_downloading_reportsProgress() async {
        let viewModel = makeViewModel(availability: .downloading(fractionComplete: 0.4))

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .downloading(fractionComplete: 0.4))
        XCTAssertFalse(viewModel.shouldPresentImmersiveSurface)
        XCTAssertEqual(viewModel.statusMessage, "Loading preview… 40%")
    }

    func test_ready_presentsTheSurfaceWithNoStatusNoise() async {
        let viewModel = makeViewModel(availability: .ready)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertTrue(viewModel.shouldPresentImmersiveSurface)
        XCTAssertNil(viewModel.statusMessage)
    }

    // MARK: - Confirmation UI

    func test_currentlySelectedStyle_showsCurrentlySelected_andDisablesReselection() {
        let viewModel = makeViewModel(availability: .unavailable, isCurrentlySelected: true)

        XCTAssertEqual(viewModel.primaryActionTitle, "Currently Selected")
        XCTAssertFalse(viewModel.isPrimaryActionEnabled)
    }

    func test_unselectedStyle_offersChooseThisStyle() {
        let viewModel = makeViewModel(availability: .unavailable, isCurrentlySelected: false)

        XCTAssertEqual(viewModel.primaryActionTitle, "Choose This Design")
        XCTAssertTrue(viewModel.isPrimaryActionEnabled)
    }

    func test_reassuranceIsShownWhenSupplied() {
        let viewModel = makeViewModel(
            availability: .unavailable,
            reassurance: "Your Rooms, photos, and content stay exactly as they are."
        )

        XCTAssertEqual(viewModel.confirmationReassurance, "Your Rooms, photos, and content stay exactly as they are.")
    }

    func test_creationContext_showsNoReassurance() {
        let viewModel = makeViewModel(availability: .unavailable, reassurance: nil)

        XCTAssertNil(viewModel.confirmationReassurance)
    }
}

struct StubPreviewAssetProvider: PreviewAssetProviding {
    let availability: PreviewAssetAvailability

    func availability(for subject: PreviewSubject) async -> PreviewAssetAvailability {
        availability
    }
}
