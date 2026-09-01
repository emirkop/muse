import XCTest
@testable import MuseApp

@MainActor
final class CollectionSharingViewModelTests: XCTestCase {
    private func makeViewModel(_ service: FakeCollectionShareLinkService, roomID: String = "cr-1") -> CollectionSharingViewModel {
        CollectionSharingViewModel(shareLinks: service, accessToken: "token", collectionRoomID: roomID)
    }

    func test_share_returnsThisRoomsLink_onTheCollectionPath() async {
        let service = FakeCollectionShareLinkService()
        let viewModel = makeViewModel(service)

        let outcome = await viewModel.shareLink()

        guard case .link(let link) = outcome else { return XCTFail("expected a link, got \(outcome)") }
        XCTAssertEqual(link.collectionRoomID, "cr-1")
        XCTAssertEqual(link.url.absoluteString, "https://muse.app/c/\(link.code)", "a Collection link lives under /c/, never /m/")
        XCTAssertEqual(service.ensureCallCount, 1)
    }

    func test_share_neverRefusesOnPrivacy_andNeverRegenerates() async {
        let service = FakeCollectionShareLinkService()
        let viewModel = makeViewModel(service)

        let first = await viewModel.shareLink()
        let second = await viewModel.shareLink()

        XCTAssertEqual(first, second, "Share reuses the active link")
        XCTAssertEqual(service.regenerateCallCount, 0)
        XCTAssertEqual(service.revokeCallCount, 0)
    }

    func test_twoRooms_haveTwoIndependentLinks() async {
        let service = FakeCollectionShareLinkService()
        let a = await makeViewModel(service, roomID: "cr-a").shareLink()
        let b = await makeViewModel(service, roomID: "cr-b").shareLink()

        guard case .link(let linkA) = a, case .link(let linkB) = b else { return XCTFail("expected two links") }
        XCTAssertNotEqual(linkA.code, linkB.code)
        XCTAssertEqual(linkA.collectionRoomID, "cr-a")
        XCTAssertEqual(linkB.collectionRoomID, "cr-b")
    }

    func test_regenerate_returnsANewLink_andTheOldOneIsDead() async {
        let service = FakeCollectionShareLinkService()
        let viewModel = makeViewModel(service)
        guard case .link(let original) = await viewModel.shareLink() else { return XCTFail() }

        let outcome = await viewModel.regenerateLink()

        guard case .link(let renewed) = outcome else { return XCTFail("expected a link, got \(outcome)") }
        XCTAssertNotEqual(renewed.code, original.code)
        XCTAssertTrue(service.deadCodes.contains(original.code))
        XCTAssertEqual(service.activeLinks["cr-1"], renewed)
    }

    func test_stopSharing_leavesNoActiveLink_andShareAgainMintsAFreshOne() async {
        let service = FakeCollectionShareLinkService()
        let viewModel = makeViewModel(service)
        guard case .link(let original) = await viewModel.shareLink() else { return XCTFail() }

        let outcome = await viewModel.stopSharing()

        XCTAssertEqual(outcome, .revoked)
        XCTAssertNil(service.activeLinks["cr-1"])
        XCTAssertTrue(service.deadCodes.contains(original.code))

        guard case .link(let fresh) = await viewModel.shareLink() else { return XCTFail() }
        XCTAssertNotEqual(fresh.code, original.code, "a revoked code never comes back")
    }

    func test_failures_areReported_andChangeNothing() async {
        let service = FakeCollectionShareLinkService()
        let viewModel = makeViewModel(service)
        guard case .link(let original) = await viewModel.shareLink() else { return XCTFail() }
        service.ownerFailure = IdentityAPIClientError.transport

        let share = await viewModel.shareLink()
        let regenerate = await viewModel.regenerateLink()
        let stop = await viewModel.stopSharing()

        XCTAssertEqual(share, .failed(message: "Couldn't get this Collection Room's link. Please try again."))
        XCTAssertEqual(regenerate, .failed(message: "Couldn't create a new link. Your current link is unchanged."))
        XCTAssertEqual(stop, .failed(message: "Couldn't stop sharing. Your current link is unchanged."))
        XCTAssertEqual(service.activeLinks["cr-1"], original, "a failed request leaves the link exactly as it was")
    }
}

@MainActor
final class CollectionShareLinkLandingViewModelTests: XCTestCase {
    private let code = "abcdefghijklmnopqrstuv"
    private let content = SharedCollectionRoomContent(
        collectionRoomID: "cr-1",
        name: "Shared Watches",
        categoryID: "category_watches",
        designID: nil,
        currentTier: .base,
        items: [CollectionItem(id: "i1", slotIndex: 0, catalogModelID: "model-1")]
    )

    func test_signedOut_enterAsksForAuthentication_withoutARequest() async {
        let service = FakeCollectionShareLinkService()
        let viewModel = CollectionShareLinkLandingViewModel(code: code, access: .signedOut, sharedRooms: service)

        XCTAssertEqual(viewModel.state, .ready, "nothing to load — there is no preview")
        XCTAssertEqual(viewModel.primaryActionTitle, "Sign in to visit")
        let outcome = await viewModel.enter()

        XCTAssertEqual(outcome, .needsAuthentication)
        XCTAssertEqual(service.visitCallCount, 0, "a signed-out recipient never triggers a content read")
    }

    func test_signedIn_enterReadsTheRoomThroughTheLink() async {
        let service = FakeCollectionShareLinkService()
        service.sharedRooms[code] = content
        let viewModel = CollectionShareLinkLandingViewModel(code: code, access: .signedIn(accessToken: "t"), sharedRooms: service)

        XCTAssertEqual(viewModel.primaryActionTitle, "Enter Collection Room")
        let outcome = await viewModel.enter()

        XCTAssertEqual(outcome, .entered(content))
        XCTAssertEqual(service.visitCallCount, 1)
    }

    func test_aDeadLink_isUnavailable() async {
        let service = FakeCollectionShareLinkService()
        let viewModel = CollectionShareLinkLandingViewModel(code: code, access: .signedIn(accessToken: "t"), sharedRooms: service)

        let outcome = await viewModel.enter()

        XCTAssertEqual(outcome, .unavailable)
        XCTAssertEqual(viewModel.state, .unavailable)
    }

    func test_aTransportFailure_isRetryable_notUnavailable() async {
        let service = FakeCollectionShareLinkService()
        service.visitFailure = IdentityAPIClientError.transport
        let viewModel = CollectionShareLinkLandingViewModel(code: code, access: .signedIn(accessToken: "t"), sharedRooms: service)

        let outcome = await viewModel.enter()

        XCTAssertEqual(outcome, .failed(message: "Couldn't enter right now. Please try again."))
        XCTAssertEqual(viewModel.state, .ready, "a network failure must not be presented as a dead link")
    }

    func test_landingCopy_namesNothing() {
        XCTAssertEqual(CollectionShareLinkLandingViewModel.heading, "You've been invited to a Collection Room.")
        XCTAssertFalse(CollectionShareLinkLandingViewModel.detail.contains(content.name))
    }
}
