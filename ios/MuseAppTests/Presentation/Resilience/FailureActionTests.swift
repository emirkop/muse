import XCTest
@testable import MuseApp

@MainActor
final class FailureActionTests: XCTestCase {

    // MARK: - Launch: the worst one

    func test_launch_connectivityFailureDoesNotInvalidateTheSession() {
        for error in [URLError(.notConnectedToInternet), URLError(.timedOut),
                      URLError(.networkConnectionLost), URLError(.dnsLookupFailed)] as [Error] {
            XCTAssertEqual(LaunchSessionRouting.verdict(for: error), .serverUnreachable,
                           "\(error) must keep the session and offer a retry")
        }
        XCTAssertEqual(LaunchSessionRouting.verdict(for: IdentityAPIClientError.offline), .serverUnreachable)
        XCTAssertEqual(LaunchSessionRouting.verdict(for: IdentityAPIClientError.transport), .serverUnreachable)
    }

    func test_launch_serverRefusalStillInvalidatesTheSession() {
        XCTAssertEqual(
            LaunchSessionRouting.verdict(for: IdentityAPIClientError.server(statusCode: 401, message: nil)),
            .sessionInvalid)
        XCTAssertEqual(
            LaunchSessionRouting.verdict(for: IdentityAPIClientError.invalidResponse),
            .sessionInvalid)
    }

    func test_launchScreen_unreachableStateIsVisibleAndRetryable() {
        let controller = LaunchLoadingViewController()
        var retried = false
        controller.onRetry = { retried = true }
        controller.showServerUnreachable(message: "You're offline.")

        let message = firstView(in: controller.view, withIdentifier: "launch-unreachable-message")
        let retry = firstView(in: controller.view, withIdentifier: "launch-retry") as? UIButton
        XCTAssertFalse(try! XCTUnwrap(message).isHidden)
        XCTAssertFalse(try! XCTUnwrap(retry).isHidden)
        retry?.sendActions(for: .touchUpInside)
        XCTAssertTrue(retried)
    }

    // MARK: - Room list

    func test_roomList_failedLoadOffersARetry() async {
        let service = FakeMuseumService()
        service.roomsResult = .failure(URLError(.notConnectedToInternet))
        let viewModel = RoomListViewModel(museumService: service, accessToken: "t")
        let controller = RoomListViewController(
            viewModel: viewModel, onCreateRoom: {}, onSelectRoom: { _ in }, onAddPhotos: { _ in },
            onEnterRoom: { _ in }, onEnterLobby: {}, onOpenRuntimeSkeleton: {}
        )
        controller.loadViewIfNeeded()
        await viewModel.load()

        let retry = firstView(in: controller.view, withIdentifier: "room-list-retry")
        XCTAssertNotNil(retry, "a failed list load needs an action — '+ New Room' is not a retry")
        XCTAssertFalse(try! XCTUnwrap(retry).isHidden)
    }

    func test_roomList_retryIsHiddenOnceLoaded() async {
        let service = FakeMuseumService()
        service.roomsResult = .failure(URLError(.timedOut))
        let viewModel = RoomListViewModel(museumService: service, accessToken: "t")
        let controller = RoomListViewController(
            viewModel: viewModel, onCreateRoom: {}, onSelectRoom: { _ in }, onAddPhotos: { _ in },
            onEnterRoom: { _ in }, onEnterLobby: {}, onOpenRuntimeSkeleton: {}
        )
        controller.loadViewIfNeeded()
        await viewModel.load()
        service.roomsResult = .success([Room(id: "r1", name: "A", variantID: "v", privacy: .private)])
        await viewModel.load()

        XCTAssertTrue(try! XCTUnwrap(firstView(in: controller.view, withIdentifier: "room-list-retry")).isHidden)
    }

    // MARK: - Profile

    func test_profile_failedLoadOffersARetryButAFailedSaveDoesNot() async {
        let service = FakeProfileService()
        service.result = .failure(URLError(.notConnectedToInternet))
        let viewModel = ProfileViewModel(profileService: service, accessToken: "t")
        let controller = ProfileViewController(viewModel: viewModel, onChangeAvatar: { _ in })
        controller.loadViewIfNeeded()

        await viewModel.load()
        XCTAssertFalse(try! XCTUnwrap(firstView(in: controller.view, withIdentifier: "profile-retry")).isHidden,
                       "a failed load has nothing to save, so Save is not its retry")

        service.result = .success(Profile(displayName: "Ada", avatarID: "avatar_1"))
        await viewModel.load()
        service.result = .failure(URLError(.notConnectedToInternet))
        await viewModel.save(displayName: "Grace")
        XCTAssertTrue(try! XCTUnwrap(firstView(in: controller.view, withIdentifier: "profile-retry")).isHidden)
    }

    // MARK: - Music

    func test_music_failedAssignmentKeepsTheLibrary() async {
        let catalog = FakeMuseumService()
        catalog.musicCatalog = [
            MusicTrack(id: "t1", displayName: "One", attribution: "a", licensing: .devTest, durationSeconds: 10),
            MusicTrack(id: "t2", displayName: "Two", attribution: "b", licensing: .devTest, durationSeconds: 12)
        ]
        let assignment = FailingMusicAssignment(error: URLError(.notConnectedToInternet))
        let viewModel = RoomMusicSelectionViewModel(
            assignedTrackID: nil, assignment: assignment, musicCatalog: catalog, accessToken: "t")
        await viewModel.load()

        await viewModel.assign(trackID: "t1")

        guard case .failed(_, let tracks) = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
        XCTAssertEqual(tracks.map { $0.id }, ["t1", "t2"],
                       "the owner must not lose the library because one assignment failed")
    }

    // MARK: - Capacity

    func test_capacity_theUpgradeProductSurvivesAFailure() async {
        let store = FakeCapacityStore()
        let entitlements = FakeEntitlementService()
        entitlements.entitlement = AccountEntitlement(state: .free, itemCapacity: 50, itemCount: 50)
        let viewModel = CapacityViewModel(
            entitlements: entitlements, store: store,
            productID: try! XCTUnwrap(store.product).id, accessToken: "t")

        await viewModel.load()
        XCTAssertNotNil(viewModel.retryableProduct)

        store.purchaseOutcome = .failed(message: "The purchase couldn't be completed.")
        await viewModel.purchase()

        guard case .failed = viewModel.state else { return XCTFail("expected .failed, got \(viewModel.state)") }
        XCTAssertNotNil(viewModel.retryableProduct,
                        "the screen must still be able to offer the upgrade at its real price")
    }

    // MARK: - Preview

    func test_preview_unreachableDoesNotClaimTheDesignWasNeverBuilt() async {
        let unbuilt = await previewMessage(for: .unavailable)
        let unreachable = await previewMessage(for: .unreachable)
        XCTAssertTrue(unbuilt.contains("hasn't been built yet"), unbuilt)
        XCTAssertFalse(unreachable.contains("hasn't been built"), unreachable)
        XCTAssertTrue(unreachable.contains("can't be reached"), unreachable)
        let choosableWhenUnreachable = await previewAllowsChoosing(.unreachable)
        let choosableWhenUnbuilt = await previewAllowsChoosing(.unavailable)
        XCTAssertTrue(choosableWhenUnreachable)
        XCTAssertTrue(choosableWhenUnbuilt)
    }

    private func previewModel(_ availability: PreviewAssetAvailability) -> PreviewViewModel {
        PreviewViewModel(
            subject: PreviewSubject(
                id: "s1", displayName: "Style",
                assetBundle: AssetBundleRef(id: "b", version: 1)),
            isCurrentlySelected: false,
            confirmationReassurance: nil,
            assetProvider: FixedPreviewAssetProvider(availability: availability)
        )
    }

    private func previewMessage(for availability: PreviewAssetAvailability) async -> String {
        let viewModel = previewModel(availability)
        await viewModel.load()
        return viewModel.statusMessage ?? "<no message>"
    }

    private func previewAllowsChoosing(_ availability: PreviewAssetAvailability) async -> Bool {
        let viewModel = previewModel(availability)
        await viewModel.load()
        return viewModel.isPrimaryActionEnabled
    }

    // MARK: - Support

    private func firstView(in root: UIView, withIdentifier identifier: String) -> UIView? {
        if root.accessibilityIdentifier == identifier { return root }
        for subview in root.subviews {
            if let found = firstView(in: subview, withIdentifier: identifier) { return found }
        }
        return nil
    }
}

// MARK: - Stubs

final class FailingMusicAssignment: MusicAssigning, @unchecked Sendable {
    private let error: Error
    init(error: Error) { self.error = error }
    func assignMusic(trackID: String) async throws -> String? { throw error }
    func removeMusic() async throws -> String? { throw error }
}

final class FixedPreviewAssetProvider: PreviewAssetProviding, @unchecked Sendable {
    private let availability: PreviewAssetAvailability
    init(availability: PreviewAssetAvailability) { self.availability = availability }
    func availability(for subject: PreviewSubject) async -> PreviewAssetAvailability { availability }
}
