import XCTest
@testable import MuseApp

@MainActor
final class CollectionDesignSelectionViewModelTests: XCTestCase {

    private static let watchesRoom = CollectionRoom(
        id: "collection-1", name: "My Watches", categoryID: "category_watches", designID: nil
    )

    private func makeViewModel(
        room: CollectionRoom = CollectionDesignSelectionViewModelTests.watchesRoom,
        catalog: FakeCollectionCatalog = FakeCollectionCatalog(),
        collections: FakeCollectionService = FakeCollectionService()
    ) -> CollectionDesignSelectionViewModel {
        collections.seed(room)
        return CollectionDesignSelectionViewModel(
            room: room, catalog: catalog, collections: collections, accessToken: "token"
        )
    }

    // MARK: - Loading, scoped by the Room's category

    func test_loadsDesignsScopedToTheRoomsOwnCategory() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.load()

        XCTAssertEqual(
            catalog.requestedCategoryIDs, ["category_watches"],
            "the list must be requested for the Room's own category — applicability is the server's rule"
        )
        guard case .loaded(let designs, let selected) = viewModel.state else {
            return XCTFail("expected .loaded, got \(viewModel.state)")
        }
        XCTAssertEqual(designs, FakeCollectionCatalog.seededDesigns)
        XCTAssertNil(selected, "the Room has no Design yet")
    }

    func test_universalDesignIsOfferedForEveryCategory() async {
        for category in FakeCollectionCatalog.seeded {
            let room = CollectionRoom(id: "c", name: "R", categoryID: category.id)
            let viewModel = makeViewModel(room: room)

            await viewModel.load()

            XCTAssertTrue(
                viewModel.availableDesigns.contains(where: { $0.isUniversal }),
                "the universal Design is missing for \(category.id)"
            )
        }
    }

    func test_scopedDesignReachesOnlyItsOwnCategory() async {
        let watchesOnly = CollectionDesign(
            id: "design_watch_case", displayName: "Watch Case",
            categoryID: "category_watches",
            assetBundle: AssetBundleRef(id: "bundle_watch_case", version: 1)
        )
        let catalog = FakeCollectionCatalog(
            designs: FakeCollectionCatalog.seededDesigns + [watchesOnly]
        )

        let watches = makeViewModel(
            room: CollectionRoom(id: "c1", name: "W", categoryID: "category_watches"), catalog: catalog
        )
        await watches.load()
        XCTAssertTrue(watches.availableDesigns.contains(watchesOnly))

        let coins = makeViewModel(
            room: CollectionRoom(id: "c2", name: "C", categoryID: "category_coins"), catalog: catalog
        )
        await coins.load()
        XCTAssertFalse(
            coins.availableDesigns.contains(watchesOnly),
            "a Watches-scoped Design must not reach a Coins Room"
        )
        XCTAssertTrue(coins.availableDesigns.contains(where: { $0.isUniversal }))
    }

    func test_noDesignsIsAnHonestStateNotAnError() async {
        let viewModel = makeViewModel(catalog: FakeCollectionCatalog(designs: []))

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .noDesignsAvailable)
        XCTAssertTrue(viewModel.availableDesigns.isEmpty)
    }

    func test_failureIsRetryable() async {
        let catalog = FakeCollectionCatalog()
        catalog.designError = URLError(.notConnectedToInternet)
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.load()
        guard case .failed = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }

        catalog.designError = nil
        await viewModel.load()
        guard case .loaded = viewModel.state else {
            return XCTFail("a retry should recover, got \(viewModel.state)")
        }
        XCTAssertEqual(catalog.designFetchCount, 2)
    }

    func test_aRoomWithNoCategoryReportsItRatherThanShowingAnEmptyPicker() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(
            room: CollectionRoom(id: "legacy", name: "Legacy", categoryID: nil), catalog: catalog
        )

        await viewModel.load()

        guard case .failed = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
        XCTAssertEqual(catalog.designFetchCount, 0, "no request can be scoped without a category")
    }

    // MARK: - Selecting

    func test_selectionPersistsThroughTheExistingPatchAndCarriesOnlyTheDesign() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.load()

        let designID = FakeCollectionCatalog.seededDesigns[0].id
        let updated = await viewModel.select(designID: designID)

        XCTAssertEqual(updated?.designID, designID)
        XCTAssertEqual(collections.patches.count, 1)
        let patch = collections.patches[0]
        XCTAssertEqual(patch.designID, designID)
        XCTAssertNil(patch.name, "a design-only patch must not carry a name")
        XCTAssertNil(patch.categoryID, "a design-only patch must not carry a category")
        XCTAssertEqual(updated?.name, "My Watches")
        XCTAssertEqual(updated?.categoryID, "category_watches")
        XCTAssertEqual(updated?.currentTier, .base)
    }

    func test_selectedDesignSurvivesAReload() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.load()
        let designID = FakeCollectionCatalog.seededDesigns[0].id
        _ = await viewModel.select(designID: designID)

        let reloaded = CollectionDesignSelectionViewModel(
            room: try! await collections.fetchCollectionRoom(accessToken: "t", collectionRoomID: "collection-1"),
            catalog: FakeCollectionCatalog(),
            collections: collections,
            accessToken: "token"
        )
        await reloaded.load()

        XCTAssertEqual(reloaded.selectedDesignID, designID)
        guard case .loaded(_, let selected) = reloaded.state else {
            return XCTFail("expected .loaded, got \(reloaded.state)")
        }
        XCTAssertEqual(selected, designID, "the picker must show the applied Design as selected")
    }

    func test_aDesignTheServerDidNotOfferIsNotSent() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.load()

        let room = await viewModel.select(designID: "design_never_offered")

        XCTAssertNil(room)
        XCTAssertTrue(collections.patches.isEmpty, "no request may be made")
    }

    func test_selectionBeforeLoadingIsNotSent() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)

        let room = await viewModel.select(designID: FakeCollectionCatalog.seededDesigns[0].id)

        XCTAssertNil(room)
        XCTAssertTrue(collections.patches.isEmpty)
    }

    func test_designNotApplicableRefusalReloadsThePicker() async {
        let catalog = FakeCollectionCatalog()
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(catalog: catalog, collections: collections)
        await viewModel.load()

        collections.updateError = CollectionAPIError(
            statusCode: 400, code: "design_not_applicable", message: "not available"
        )
        let room = await viewModel.select(designID: FakeCollectionCatalog.seededDesigns[0].id)

        XCTAssertNil(room)
        XCTAssertNotNil(viewModel.selectionErrorMessage)
        XCTAssertEqual(catalog.designFetchCount, 2, "a stale Design list must be refreshed")
        XCTAssertNil(viewModel.selectedDesignID)
    }

    func test_otherRefusalsDoNotReloadThePicker() async {
        let catalog = FakeCollectionCatalog()
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(catalog: catalog, collections: collections)
        await viewModel.load()

        collections.updateError = CollectionAPIError(statusCode: 500, code: nil, message: nil)
        let room = await viewModel.select(designID: FakeCollectionCatalog.seededDesigns[0].id)

        XCTAssertNil(room)
        XCTAssertNotNil(viewModel.selectionErrorMessage)
        XCTAssertEqual(catalog.designFetchCount, 1, "a server fault says nothing about the Design list")
    }

    func test_isSavingIsClearedOnBothOutcomes() async {
        let collections = FakeCollectionService()
        let viewModel = makeViewModel(collections: collections)
        await viewModel.load()
        let designID = FakeCollectionCatalog.seededDesigns[0].id

        _ = await viewModel.select(designID: designID)
        XCTAssertFalse(viewModel.isSaving)

        collections.updateError = CollectionAPIError(statusCode: 500, code: nil, message: nil)
        _ = await viewModel.select(designID: designID)
        XCTAssertFalse(viewModel.isSaving, "a failure must not leave the screen spinning for ever")
    }

    // MARK: - Labelling placeholder content

    func test_aDevelopmentFixtureIsLabelled() async {
        let viewModel = makeViewModel()
        await viewModel.load()

        let fixture = viewModel.availableDesigns[0]
        XCTAssertTrue(fixture.isDevelopmentFixture)
        let subtitle = viewModel.subtitle(for: fixture)
        XCTAssertNotNil(subtitle)
        XCTAssertTrue(
            subtitle!.contains("Development placeholder"),
            "a fixture's subtitle must say it is not a finished design, got \(subtitle!)"
        )
        XCTAssertTrue(fixture.displayName.contains("Development"))
    }

    func test_aRealUniversalDesignSaysItWorksAnywhere() async {
        let real = CollectionDesign(
            id: "design_minimal", displayName: "Minimal",
            categoryID: nil, isDevelopmentFixture: false,
            assetBundle: AssetBundleRef(id: "bundle_minimal", version: 1)
        )
        let viewModel = makeViewModel(catalog: FakeCollectionCatalog(designs: [real]))
        await viewModel.load()

        XCTAssertEqual(viewModel.subtitle(for: real), "Works with any collection")
    }

    func test_aScopedProductionDesignHasNoSubtitle() async {
        let scoped = CollectionDesign(
            id: "design_watch_case", displayName: "Watch Case",
            categoryID: "category_watches", isDevelopmentFixture: false,
            assetBundle: AssetBundleRef(id: "bundle_watch_case", version: 1)
        )
        let viewModel = makeViewModel(catalog: FakeCollectionCatalog(designs: [scoped]))
        await viewModel.load()

        XCTAssertNil(viewModel.subtitle(for: scoped))
    }

    // MARK: - Preview, through the shared /32/53 mechanism

    func test_previewSubjectCarriesTheDesignsOwnBundleIdentity() {
        let design = FakeCollectionCatalog.seededDesigns[0]

        let subject = design.previewSubject

        XCTAssertEqual(subject.id, design.id)
        XCTAssertEqual(subject.displayName, design.displayName)
        XCTAssertEqual(subject.assetBundle, design.assetBundle)
        XCTAssertEqual(subject.assetBundle.id, "dev_fixture_collection_design")
        XCTAssertEqual(subject.assetBundle.version, 1)
    }

    func test_previewUsesTheSharedMechanismAndReportsProviderStates() async {
        let design = FakeCollectionCatalog.seededDesigns[0]

        let unavailable = PreviewViewModel(
            subject: design.previewSubject,
            isCurrentlySelected: false,
            confirmationReassurance: CollectionDesignSelectionViewModel.previewReassurance,
            assetProvider: UnavailablePreviewAssetProvider()
        )
        await unavailable.load()
        XCTAssertEqual(unavailable.state, .assetsUnavailable)

        let ready = PreviewViewModel(
            subject: design.previewSubject,
            isCurrentlySelected: true,
            confirmationReassurance: nil,
            assetProvider: StubPreviewAssetProvider(availability: .ready)
        )
        await ready.load()
        XCTAssertEqual(ready.state, .ready)

        let downloading = PreviewViewModel(
            subject: design.previewSubject,
            isCurrentlySelected: false,
            confirmationReassurance: nil,
            assetProvider: StubPreviewAssetProvider(availability: .downloading(fractionComplete: 0.4))
        )
        await downloading.load()
        XCTAssertEqual(downloading.state, .downloading(fractionComplete: 0.4))
    }

    func test_previewReassuranceIsAboutItemsNotPhotos() {
        let copy = CollectionDesignSelectionViewModel.previewReassurance
        XCTAssertTrue(copy.contains("items"), "the reassurance must be about a collection's items")
        XCTAssertFalse(copy.contains("photo"), "photos are the Museum's concern, not a Collection Room's")
    }
}
