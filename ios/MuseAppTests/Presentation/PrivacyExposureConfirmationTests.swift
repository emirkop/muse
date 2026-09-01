import XCTest
@testable import MuseApp

@MainActor
final class PrivacyExposureConfirmationTests: XCTestCase {
    private var window: UIWindow?

    override func tearDown() {
        window?.isHidden = true
        window = nil
        super.tearDown()
    }

    private final class ConfirmationSpy {
        private(set) var asked: [PrivacySettingsViewModel.ExposureConfirmation] = []
        var answer = true

        func presenter() -> PrivacySettingsViewController.ConfirmationPresenting {
            { [self] confirmation, respond in
                asked.append(confirmation)
                respond(answer)
            }
        }
    }

    private func makeScreen(
        museumPrivacy: MusePrivacy,
        rooms: [Room] = [Room(id: "r1", name: "The Long Hall", variantID: "v1", privacy: .private)]
    ) -> (PrivacySettingsViewController, PrivacySettingsViewModel, FakeMuseumService) {
        let service = FakeMuseumService()
        service.fetchResult = .success(Museum(id: "m1", styleID: "style_modern", privacy: museumPrivacy))
        service.roomsResult = .success(rooms)
        let viewModel = PrivacySettingsViewModel(museumService: service, accessToken: "token")
        let viewController = PrivacySettingsViewController(viewModel: viewModel)
        viewController.loadViewIfNeeded()
        return (viewController, viewModel, service)
    }

    private func settle() async {
        for _ in 0..<20 { await Task.yield() }
    }

    // MARK: - Museum level

    func test_makingTheMuseumPublic_asksFirst_andOnlyThenApplies() async {
        let spy = ConfirmationSpy()
        let (viewController, viewModel, service) = makeScreen(museumPrivacy: .private)
        viewController.confirmationPresenter = spy.presenter()
        await viewModel.load()

        viewController.testFlipSwitch(labelled: "Museum privacy", to: true)
        await settle()

        XCTAssertEqual(spy.asked.count, 1, "exposing content must be confirmed")
        XCTAssertEqual(spy.asked.first?.title, "Make your Museum Public?")
        XCTAssertEqual(spy.asked.first?.confirmTitle, "Make Public")
        XCTAssertEqual(service.receivedMuseumPrivacies, [.public])
    }

    func test_decliningTheConfirmation_changesNothing_andRestoresTheSwitch() async {
        let spy = ConfirmationSpy()
        spy.answer = false
        let (viewController, viewModel, service) = makeScreen(museumPrivacy: .private)
        viewController.confirmationPresenter = spy.presenter()
        await viewModel.load()

        viewController.testFlipSwitch(labelled: "Museum privacy", to: true)
        await settle()

        XCTAssertEqual(spy.asked.count, 1)
        XCTAssertEqual(service.changePrivacyCallCount, 0, "a declined change must reach no endpoint")
        XCTAssertEqual(viewController.testSwitchStates["Museum privacy"], false,
                       "the switch must show the state that is actually still in force")
    }

    func test_makingTheMuseumPrivate_appliesWithoutAsking() async {
        let spy = ConfirmationSpy()
        let (viewController, viewModel, service) = makeScreen(museumPrivacy: .public)
        viewController.confirmationPresenter = spy.presenter()
        await viewModel.load()

        viewController.testFlipSwitch(labelled: "Museum privacy", to: false)
        await settle()

        XCTAssertTrue(spy.asked.isEmpty)
        XCTAssertEqual(service.receivedMuseumPrivacies, [.private])
    }

    // MARK: - Room level

    func test_makingARoomPublicInsideAPrivateMuseum_asksNothing() async {
        let spy = ConfirmationSpy()
        let (viewController, viewModel, service) = makeScreen(museumPrivacy: .private)
        viewController.confirmationPresenter = spy.presenter()
        await viewModel.load()

        viewController.testFlipSwitch(labelled: "The Long Hall privacy", to: true)
        await settle()

        XCTAssertTrue(spy.asked.isEmpty)
        XCTAssertEqual(service.receivedRoomPatches, [RoomPatch(privacy: .public)])
    }

    func test_makingARoomPublicInsideAPublicMuseum_asksFirst() async {
        let spy = ConfirmationSpy()
        let (viewController, viewModel, service) = makeScreen(museumPrivacy: .public)
        viewController.confirmationPresenter = spy.presenter()
        await viewModel.load()

        viewController.testFlipSwitch(labelled: "The Long Hall privacy", to: true)
        await settle()

        XCTAssertEqual(spy.asked.first?.title, "Make “The Long Hall” Public?")
        XCTAssertEqual(service.receivedRoomPatches, [RoomPatch(privacy: .public)])
    }

    func test_makingARoomPrivate_appliesWithoutAsking() async {
        let spy = ConfirmationSpy()
        let rooms = [Room(id: "r1", name: "The Long Hall", variantID: "v1", privacy: .public)]
        let (viewController, viewModel, service) = makeScreen(museumPrivacy: .public, rooms: rooms)
        viewController.confirmationPresenter = spy.presenter()
        await viewModel.load()

        viewController.testFlipSwitch(labelled: "The Long Hall privacy", to: false)
        await settle()

        XCTAssertTrue(spy.asked.isEmpty)
        XCTAssertEqual(service.receivedRoomPatches, [RoomPatch(privacy: .private)])
    }

    // MARK: - The real alert

    func test_withoutAnInjectedPresenter_arealAlertStandsBeforeAnyRequest() async {
        let (viewController, viewModel, service) = makeScreen(museumPrivacy: .private)
        let window: UIWindow
        if let scene = UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).first {
            window = UIWindow(windowScene: scene)
        } else {
            window = UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 852))
        }
        window.rootViewController = viewController
        window.makeKeyAndVisible()
        self.window = window

        for _ in 0..<100 where !viewModel.hasLoadedContent {
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        XCTAssertTrue(viewModel.hasLoadedContent, "the appearance-triggered load did not publish within 2s")

        viewController.testFlipSwitch(labelled: "Museum privacy", to: true)

        var alert: UIAlertController?
        for _ in 0..<50 where alert == nil {
            alert = viewController.presentedViewController as? UIAlertController
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        XCTAssertEqual(alert?.title, "Make your Museum Public?")
        XCTAssertEqual(alert?.actions.count, 2)
        XCTAssertEqual(alert?.actions.map(\.title), ["Cancel", "Make Public"])
        XCTAssertEqual(service.changePrivacyCallCount, 0,
                       "the request must wait for the owner's answer")
    }
}

private extension PrivacySettingsViewModel {
    var hasLoadedContent: Bool {
        if case .loaded = state { return true }
        return false
    }
}
