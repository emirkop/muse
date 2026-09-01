import XCTest
@testable import MuseApp

@MainActor
final class AppCoordinatorCollectionDeepLinkTests: XCTestCase {
    private let code = "abcdefghijklmnopqrstuv"
    private let collectionURL = URL(string: "https://muse.app/c/abcdefghijklmnopqrstuv")!
    private let museumURL = URL(string: "https://muse.app/m/ZZZZZZZZZZZZZZZZZZZZZZ")!
    private let content = SharedCollectionRoomContent(
        collectionRoomID: "cr-1",
        name: "Shared Watches",
        categoryID: "category_watches",
        designID: nil,
        currentTier: .base,
        items: [CollectionItem(id: "i1", slotIndex: 0, catalogModelID: "model-1")]
    )

    override func setUp() {
        super.setUp()
        MainActor.assumeIsolated { UIView.setAnimationsEnabled(false) }
    }

    override func tearDown() {
        MainActor.assumeIsolated { UIView.setAnimationsEnabled(true) }
        super.tearDown()
    }

    private func makeCoordinator() -> AppCoordinator {
        let window: UIWindow
        if let scene = UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).first {
            window = UIWindow(windowScene: scene)
        } else {
            window = UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 852))
        }
        return AppCoordinator(window: window)
    }

    private func loginResult(isNewAccount: Bool) -> LoginResult {
        LoginResult(
            session: AuthSession(
                accessToken: "access",
                accessTokenExpiresAt: Date(timeIntervalSinceNow: 900),
                refreshToken: "refresh",
                refreshTokenExpiresAt: Date(timeIntervalSinceNow: 86_400)
            ),
            isNewAccount: isNewAccount
        )
    }

    private func settle() {
        RunLoop.main.run(until: Date(timeIntervalSinceNow: 0.05))
    }

    // A navigation transition is not guaranteed to finish inside a fixed
    // sleep: on a loaded machine it can take longer, and the test then reads
    // a stack that is still mid-transition. Wait for the state instead.
    private func settle(until reached: () -> Bool, timeout: TimeInterval = 3) {
        let deadline = Date(timeIntervalSinceNow: timeout)
        while !reached() && Date() < deadline {
            RunLoop.main.run(until: Date(timeIntervalSinceNow: 0.02))
        }
    }

    private func topIs<T: UIViewController>(_ coordinator: AppCoordinator, _ type: T.Type, _ message: String = "") {
        settle(until: { coordinator.testViewControllers.last is T })
        XCTAssertTrue(coordinator.testViewControllers.last is T,
                      "expected \(T.self) on top, got \(coordinator.testViewControllers.map { String(describing: Swift.type(of: $0)) }) \(message)")
    }

    // MARK: - Arrival

    func test_coldLaunchSignedOut_collectionLandingComesFirst() {
        let coordinator = makeCoordinator()

        XCTAssertTrue(coordinator.handleIncomingURL(collectionURL))
        XCTAssertEqual(coordinator.testPendingShareLink, .collectionRoom(code: code))
        coordinator.testCompleteLaunchRouting(accessToken: nil)

        XCTAssertEqual(coordinator.testViewControllers.count, 1)
        topIs(coordinator, CollectionShareLinkLandingViewController.self)
        XCTAssertFalse(coordinator.testViewControllers.contains { $0 is ShareLinkLandingViewController },
                       "a Collection link must never open the Museum landing")
    }

    func test_coldLaunchSignedIn_collectionLandingAboveTheHub() {
        let coordinator = makeCoordinator()

        XCTAssertTrue(coordinator.handleIncomingURL(collectionURL))
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        settle()

        XCTAssertTrue(coordinator.testViewControllers.first is MainProductChoiceViewController)
        topIs(coordinator, CollectionShareLinkLandingViewController.self)
    }

    func test_aLinkOfTheOtherKind_replacesTheLanding() {
        let coordinator = makeCoordinator()
        coordinator.testCompleteLaunchRouting(accessToken: "token")

        coordinator.handleIncomingURL(museumURL)
        settle()
        coordinator.handleIncomingURL(collectionURL)
        settle()

        XCTAssertEqual(coordinator.testPendingShareLink, .collectionRoom(code: code))
        let landings = coordinator.testViewControllers.filter {
            $0 is ShareLinkLandingViewController || $0 is CollectionShareLinkLandingViewController
        }
        XCTAssertEqual(landings.count, 1)
        topIs(coordinator, CollectionShareLinkLandingViewController.self)

        coordinator.handleIncomingURL(museumURL)
        settle()
        XCTAssertEqual(coordinator.testPendingShareLink, .museum(code: "ZZZZZZZZZZZZZZZZZZZZZZ"))
        topIs(coordinator, ShareLinkLandingViewController.self)
        XCTAssertEqual(coordinator.testViewControllers.filter { $0 is CollectionShareLinkLandingViewController }.count, 0)
    }

    // MARK: - The detour

    func test_signInFromCollectionLanding_returnsToTheCollectionLanding() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(collectionURL)
        coordinator.testCompleteLaunchRouting(accessToken: nil)
        topIs(coordinator, CollectionShareLinkLandingViewController.self)

        coordinator.testStartAuthenticationFromLanding()
        topIs(coordinator, LogInViewController.self)
        XCTAssertEqual(coordinator.testPendingShareLink, .collectionRoom(code: code), "the link survives the detour")

        coordinator.testCompleteAuthentication(loginResult(isNewAccount: false))

        topIs(coordinator, CollectionShareLinkLandingViewController.self, "back to the Collection landing, not the hub, not the Museum landing")
        XCTAssertTrue(coordinator.testViewControllers.first is MainProductChoiceViewController)
        XCTAssertEqual(coordinator.testPendingShareLink, .collectionRoom(code: code), "still pending until the visitor actually enters")
    }

    func test_signUpFromCollectionLanding_onboardsFirst_thenReturns() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(collectionURL)
        coordinator.testCompleteLaunchRouting(accessToken: nil)
        coordinator.testStartAuthenticationFromLanding()

        coordinator.testCompleteAuthentication(loginResult(isNewAccount: true))
        topIs(coordinator, AccountCreationViewController.self)
        XCTAssertEqual(coordinator.testPendingShareLink, .collectionRoom(code: code))

        coordinator.testCompleteAvatarOnboarding()

        topIs(coordinator, CollectionShareLinkLandingViewController.self)
    }

    func test_dismiss_clearsTheCollectionLink() {
        let coordinator = makeCoordinator()
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        coordinator.handleIncomingURL(collectionURL)
        settle()

        coordinator.testDismissPendingShareLink()
        settle()

        XCTAssertNil(coordinator.testPendingShareLink)
        XCTAssertFalse(coordinator.testViewControllers.contains { $0 is CollectionShareLinkLandingViewController })
    }

    // MARK: - Entry

    func test_visitorWithAnAvatar_entersTheViewOnlyRoom() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(collectionURL)
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        settle()

        coordinator.testContinueCollectionVisitorEntry(code: code, content: content, avatarID: "avatar_3")
        settle()

        XCTAssertFalse(coordinator.testViewControllers.contains { $0 is AvatarSelectionViewController })
        topIs(coordinator, SharedCollectionRoomViewController.self)
        let shown = coordinator.testViewControllers.compactMap { $0 as? SharedCollectionRoomViewController }.last
        XCTAssertEqual(shown?.testContent, content, "exactly the Room the link returned")
        XCTAssertNil(coordinator.testPendingShareLink, "the link has been consumed")
        XCTAssertFalse(coordinator.testViewControllers.contains { $0 is CollectionRoomListViewController },
                       "a visitor never reaches the owner's Collection Rooms")
    }

    func test_visitorWithoutAnAvatar_choosesOneFirst_thenEnters() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(collectionURL)
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        settle()

        coordinator.testContinueCollectionVisitorEntry(code: code, content: content, avatarID: "")

        topIs(coordinator, AvatarSelectionViewController.self)
        XCTAssertFalse(coordinator.testViewControllers.contains { $0 is AccountCreationViewController })
        XCTAssertFalse(coordinator.testViewControllers.contains { $0 is SharedCollectionRoomViewController })
        XCTAssertEqual(coordinator.testPendingShareLink, .collectionRoom(code: code), "still pending while the Avatar is chosen")

        let avatar = coordinator.testViewControllers.compactMap { $0 as? AvatarSelectionViewController }.last
        avatar?.testCompleteSelection(avatarID: "avatar_2")
        settle()

        topIs(coordinator, SharedCollectionRoomViewController.self)
        XCTAssertNil(coordinator.testPendingShareLink)
    }
}
