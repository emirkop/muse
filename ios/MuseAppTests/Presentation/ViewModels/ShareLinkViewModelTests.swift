import XCTest
@testable import MuseApp

@MainActor
final class MuseumSharingViewModelTests: XCTestCase {
    private let publicMuseum = Museum(id: "m1", styleID: "style_modern", privacy: .public)
    private let privateMuseum = Museum(id: "m1", styleID: "style_modern", privacy: .private)

    private func makeViewModel(_ service: FakeShareLinkService) -> MuseumSharingViewModel {
        MuseumSharingViewModel(shareLinkService: service, accessToken: "token")
    }

    func test_sharingAPrivateMuseum_clarifiesInsteadOfMintingALink() async {
        let service = FakeShareLinkService()
        let viewModel = makeViewModel(service)

        let outcome = await viewModel.shareLink(for: privateMuseum)

        XCTAssertEqual(outcome, .museumIsPrivate)
        XCTAssertEqual(service.ensureCallCount, 0)
        XCTAssertNil(service.activeLink)
    }

    func test_sharingAPublicMuseum_returnsTheLink() async {
        let service = FakeShareLinkService()
        let viewModel = makeViewModel(service)

        let outcome = await viewModel.shareLink(for: publicMuseum)

        guard case .link(let link) = outcome else { return XCTFail("expected a link, got \(outcome)") }
        XCTAssertEqual(link.url.absoluteString, "https://muse.app/m/\(link.code)")
        XCTAssertEqual(service.ensureCallCount, 1)
    }

    func test_sharingTwice_yieldsTheSameLink() async {
        let service = FakeShareLinkService()
        let viewModel = makeViewModel(service)

        let first = await viewModel.shareLink(for: publicMuseum)
        let second = await viewModel.shareLink(for: publicMuseum)

        XCTAssertEqual(first, second)
        XCTAssertEqual(service.regenerateCallCount, 0, "Share Museum must never regenerate")
    }

    func test_sharingFailure_isReported() async {
        let service = FakeShareLinkService()
        service.ensureResult = .failure(IdentityAPIClientError.transport)
        let viewModel = makeViewModel(service)

        let outcome = await viewModel.shareLink(for: publicMuseum)

        XCTAssertEqual(outcome, .failed(message: "Couldn't get your Museum's link. Please try again."))
    }

    func test_regenerate_returnsANewLink_andWorksWhilePrivate() async {
        let service = FakeShareLinkService()
        let viewModel = makeViewModel(service)
        guard case .link(let old) = await viewModel.shareLink(for: publicMuseum) else { return XCTFail() }

        let outcome = await viewModel.regenerateLink()

        guard case .link(let fresh) = outcome else { return XCTFail("expected a link, got \(outcome)") }
        XCTAssertNotEqual(fresh.code, old.code)
        XCTAssertEqual(service.regenerateCallCount, 1)
        XCTAssertEqual(service.activeLink, fresh, "the new link is the active one")
    }

    func test_regenerateFailure_saysTheCurrentLinkIsUnchanged() async {
        let service = FakeShareLinkService()
        service.regenerateResult = .failure(IdentityAPIClientError.transport)
        let viewModel = makeViewModel(service)

        let outcome = await viewModel.regenerateLink()

        XCTAssertEqual(outcome, .failed(message: "Couldn't create a new link. Your current link is unchanged."))
    }
}

@MainActor
final class ShareLinkLandingViewModelTests: XCTestCase {
    private let code = "abcdefghijklmnopqrstuv"
    private let preview = ShareLinkPreview(code: "abcdefghijklmnopqrstuv", styleID: "style_modern", ownerAvatarID: "avatar_2")

    private func makeViewModel(access: ShareLinkLandingViewModel.Access, service: FakeShareLinkService) -> ShareLinkLandingViewModel {
        ShareLinkLandingViewModel(code: code, access: access, shareLinkService: service)
    }

    func test_load_showsThePreview_withoutAToken() async {
        let service = FakeShareLinkService()
        service.previewResult = .success(preview)
        let viewModel = makeViewModel(access: .signedOut, service: service)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .available(preview))
        XCTAssertEqual(service.previewedCodes, [code])
        XCTAssertTrue(service.sharedMuseumRequests.isEmpty, "no content moves before authentication")
    }

    func test_load_404_isTheUnavailableState() async {
        let viewModel = makeViewModel(access: .signedOut, service: FakeShareLinkService())

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .unavailable)
    }

    func test_load_transportFailure_isFailed_notUnavailable() async {
        let service = FakeShareLinkService()
        service.previewResult = .failure(IdentityAPIClientError.transport)
        let viewModel = makeViewModel(access: .signedOut, service: service)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .failed(message: "Couldn't open this link. Please try again."))
    }

    func test_primaryAction_dependsOnAccess() {
        XCTAssertEqual(makeViewModel(access: .signedOut, service: FakeShareLinkService()).primaryActionTitle, "Sign in to visit")
        XCTAssertEqual(makeViewModel(access: .signedIn(accessToken: "t"), service: FakeShareLinkService()).primaryActionTitle, "Enter Museum")
    }

    func test_enter_signedOut_asksForAuthentication_andRequestsNothing() async {
        let service = FakeShareLinkService()
        let viewModel = makeViewModel(access: .signedOut, service: service)

        let outcome = await viewModel.enter()

        XCTAssertEqual(outcome, .needsAuthentication)
        XCTAssertTrue(service.sharedMuseumRequests.isEmpty)
    }

    func test_enter_signedIn_readsThroughTheLink_withTheToken() async {
        let service = FakeShareLinkService()
        let content = SharedMuseumContent(museumID: "m1", styleID: "style_modern", rooms: [SharedRoomSummary(id: "r1", name: "Hall", variantID: "v1")])
        service.sharedMuseumResult = .success(content)
        let viewModel = makeViewModel(access: .signedIn(accessToken: "token"), service: service)

        let outcome = await viewModel.enter()

        XCTAssertEqual(outcome, .entered(content))
        XCTAssertEqual(service.sharedMuseumRequests.count, 1)
        XCTAssertEqual(service.sharedMuseumRequests.first?.token, "token")
        XCTAssertEqual(service.sharedMuseumRequests.first?.code, code)
    }

    func test_enter_404_becomesUnavailable() async {
        let service = FakeShareLinkService()
        service.previewResult = .success(preview)
        let viewModel = makeViewModel(access: .signedIn(accessToken: "token"), service: service)
        await viewModel.load()

        let outcome = await viewModel.enter()

        XCTAssertEqual(outcome, .unavailable)
        XCTAssertEqual(viewModel.state, .unavailable)
    }

    func test_heading_isFixed_andNamesNobody() {
        XCTAssertEqual(ShareLinkLandingViewModel.heading, "You've been invited to a Museum.")
    }
}
