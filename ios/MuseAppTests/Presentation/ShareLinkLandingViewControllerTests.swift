import XCTest
@testable import MuseApp

@MainActor
final class ShareLinkLandingViewControllerTests: XCTestCase {
    private let code = "abcdefghijklmnopqrstuv"
    private let preview = ShareLinkPreview(code: "abcdefghijklmnopqrstuv", styleID: "style_modern", ownerAvatarID: "avatar_2")

    private func makeScreen(access: ShareLinkLandingViewModel.Access, service: FakeShareLinkService) -> (ShareLinkLandingViewController, ShareLinkLandingViewModel, Recorder) {
        let recorder = Recorder()
        let viewModel = ShareLinkLandingViewModel(code: code, access: access, shareLinkService: service)
        let controller = ShareLinkLandingViewController(
            viewModel: viewModel,
            onSignIn: { recorder.signIns += 1 },
            onEntered: { recorder.entered.append($0) },
            onDismiss: { recorder.dismissed += 1 }
        )
        controller.loadViewIfNeeded()
        return (controller, viewModel, recorder)
    }

    private final class Recorder {
        var signIns = 0
        var entered: [SharedMuseumContent] = []
        var dismissed = 0
    }

    private func settle() async {
        for _ in 0..<20 { await Task.yield() }
    }

    func test_signedOut_showsThePreview_andOffersSignIn() async {
        let service = FakeShareLinkService()
        service.previewResult = .success(preview)
        let (controller, viewModel, recorder) = makeScreen(access: .signedOut, service: service)
        await viewModel.load()

        XCTAssertEqual(controller.testHeadingText, "You've been invited to a Museum.")
        XCTAssertEqual(controller.testDetailText, "Avatar: Avatar 2\nStyle: style_modern")
        XCTAssertEqual(controller.testPrimaryTitle, "Sign in to visit")

        controller.testTapPrimary()
        await settle()

        XCTAssertEqual(recorder.signIns, 1, "signed out: the primary action starts Authentication")
        XCTAssertTrue(service.sharedMuseumRequests.isEmpty, "and requests no content")
    }

    func test_signedIn_entersThroughTheLink() async {
        let service = FakeShareLinkService()
        service.previewResult = .success(preview)
        let content = SharedMuseumContent(museumID: "m1", styleID: "style_modern", rooms: [])
        service.sharedMuseumResult = .success(content)
        let (controller, viewModel, recorder) = makeScreen(access: .signedIn(accessToken: "token"), service: service)
        await viewModel.load()
        XCTAssertEqual(controller.testPrimaryTitle, "Enter Museum")

        controller.testTapPrimary()
        for _ in 0..<50 where recorder.entered.isEmpty { await settle() }

        XCTAssertEqual(recorder.entered, [content])
        XCTAssertEqual(recorder.signIns, 0)
    }

    func test_unavailable_showsTheExactCopy_andOnlyAWayOut() async {
        let (controller, viewModel, recorder) = makeScreen(access: .signedOut, service: FakeShareLinkService())
        await viewModel.load()

        XCTAssertEqual(controller.testHeadingText, "This link is no longer available.")
        XCTAssertEqual(controller.testPrimaryTitle, "OK")

        controller.testTapPrimary()

        XCTAssertEqual(recorder.dismissed, 1)
        XCTAssertEqual(recorder.signIns, 0, "an unavailable link never leads into sign-in")
    }

    func test_screenRendersNothingBeyondThePreviewFields() async {
        let service = FakeShareLinkService()
        service.previewResult = .success(preview)
        let (controller, viewModel, _) = makeScreen(access: .signedOut, service: service)
        await viewModel.load()

        let shown = [controller.testHeadingText, controller.testDetailText].compactMap { $0 }.joined(separator: " ")
        for forbidden in ["Emir", "Room", "photo", "m1", "private", "Private"] {
            XCTAssertFalse(shown.contains(forbidden), "landing must not show \(forbidden): \(shown)")
        }
    }
}
