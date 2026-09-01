import XCTest
@testable import MuseApp

@MainActor
final class AppCoordinatorDeepLinkTests: XCTestCase {
    private let shareURL = URL(string: "https://muse.app/m/abcdefghijklmnopqrstuv")!
    private let otherShareURL = URL(string: "https://muse.app/m/ZZZZZZZZZZZZZZZZZZZZZZ")!

    override func setUp() async throws {
        try await super.setUp()
        UIView.setAnimationsEnabled(false)
    }

    override func tearDown() async throws {
        UIView.setAnimationsEnabled(true)
        try await super.tearDown()
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
        RunLoop.main.run(until: Date(timeIntervalSinceNow: 0.1))
    }

    private func topIs<T: UIViewController>(_ coordinator: AppCoordinator, _ type: T.Type, _ message: String = "") {
        settle()
        XCTAssertTrue(coordinator.testViewControllers.last is T,
                      "expected \(T.self) on top, got \(coordinator.testViewControllers.map { String(describing: Swift.type(of: $0)) }) \(message)")
    }

    // MARK: - Arrival

    func test_coldLaunchSignedOut_landingComesFirst() {
        let coordinator = makeCoordinator()

        XCTAssertTrue(coordinator.handleIncomingURL(shareURL))
        XCTAssertEqual(coordinator.testPendingShareLinkCode, "abcdefghijklmnopqrstuv")
        coordinator.testCompleteLaunchRouting(accessToken: nil)

        XCTAssertEqual(coordinator.testViewControllers.count, 1)
        topIs(coordinator, ShareLinkLandingViewController.self)
    }

    func test_coldLaunchSignedIn_landingAboveTheHub() {
        let coordinator = makeCoordinator()

        XCTAssertTrue(coordinator.handleIncomingURL(shareURL))
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        settle()

        XCTAssertTrue(coordinator.testViewControllers.first is MainProductChoiceViewController)
        topIs(coordinator, ShareLinkLandingViewController.self)
    }

    func test_linkWhileRunning_presentsImmediately() {
        let coordinator = makeCoordinator()
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        XCTAssertTrue(coordinator.testViewControllers.first is MainProductChoiceViewController)

        XCTAssertTrue(coordinator.handleIncomingURL(shareURL))

        topIs(coordinator, ShareLinkLandingViewController.self)
    }

    func test_secondLink_replacesTheLanding() {
        let coordinator = makeCoordinator()
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        coordinator.handleIncomingURL(shareURL)

        coordinator.handleIncomingURL(otherShareURL)
        settle()

        XCTAssertEqual(coordinator.testPendingShareLinkCode, "ZZZZZZZZZZZZZZZZZZZZZZ")
        XCTAssertEqual(coordinator.testViewControllers.filter { $0 is ShareLinkLandingViewController }.count, 1)
    }

    func test_foreignOrMalformedURL_isIgnored() {
        let coordinator = makeCoordinator()
        coordinator.testCompleteLaunchRouting(accessToken: nil)
        let before = coordinator.testViewControllers.map { ObjectIdentifier($0) }

        XCTAssertFalse(coordinator.handleIncomingURL(URL(string: "https://evil.example/m/abcdefghijklmnopqrstuv")!))
        XCTAssertFalse(coordinator.handleIncomingURL(URL(string: "https://muse.app/museums/abcdefghijklmnopqrstuv")!))
        XCTAssertFalse(coordinator.handleIncomingURL(URL(string: "https://muse.app/m/short")!))

        XCTAssertNil(coordinator.testPendingShareLinkCode)
        XCTAssertEqual(coordinator.testViewControllers.map { ObjectIdentifier($0) }, before)
    }

    // MARK: - The detour

    func test_signInFromLanding_returnsToTheLanding_notHome() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(shareURL)
        coordinator.testCompleteLaunchRouting(accessToken: nil)
        topIs(coordinator, ShareLinkLandingViewController.self)

        coordinator.testStartAuthenticationFromLanding()
        topIs(coordinator, LogInViewController.self, "sign-in is the ordinary Log In screen")
        XCTAssertEqual(coordinator.testPendingShareLinkCode, "abcdefghijklmnopqrstuv", "the link survives the detour")

        coordinator.testCompleteAuthentication(loginResult(isNewAccount: false))

        topIs(coordinator, ShareLinkLandingViewController.self, "back to the landing, not the hub")
        XCTAssertTrue(coordinator.testViewControllers.first is MainProductChoiceViewController, "with home underneath now that there is a session")
        XCTAssertEqual(coordinator.testPendingShareLinkCode, "abcdefghijklmnopqrstuv", "still pending until the visitor actually enters")
    }

    func test_signUpFromLanding_onboardsFirst_thenReturnsToTheLanding() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(shareURL)
        coordinator.testCompleteLaunchRouting(accessToken: nil)
        coordinator.testStartAuthenticationFromLanding()

        coordinator.testCompleteAuthentication(loginResult(isNewAccount: true))
        topIs(coordinator, AccountCreationViewController.self, "a new account onboards before visiting")
        XCTAssertEqual(coordinator.testPendingShareLinkCode, "abcdefghijklmnopqrstuv", "the link survives onboarding")

        coordinator.testCompleteAvatarOnboarding()

        topIs(coordinator, ShareLinkLandingViewController.self, "and lands on the link afterwards")
    }

    func test_withoutAPendingLink_authenticationGoesHome() {
        let coordinator = makeCoordinator()
        coordinator.testCompleteLaunchRouting(accessToken: nil)

        coordinator.testCompleteAuthentication(loginResult(isNewAccount: false))
        topIs(coordinator, MainProductChoiceViewController.self)

        let fresh = makeCoordinator()
        fresh.testCompleteLaunchRouting(accessToken: nil)
        fresh.testCompleteAuthentication(loginResult(isNewAccount: true))
        settle()
        fresh.testCompleteAvatarOnboarding()
        topIs(fresh, MainProductChoiceViewController.self)
    }

    func test_dismiss_clearsTheLink() {
        let signedOut = makeCoordinator()
        signedOut.handleIncomingURL(shareURL)
        signedOut.testCompleteLaunchRouting(accessToken: nil)
        signedOut.testDismissPendingShareLink()
        XCTAssertNil(signedOut.testPendingShareLinkCode)
        topIs(signedOut, LogInViewController.self)

        let signedIn = makeCoordinator()
        signedIn.testCompleteLaunchRouting(accessToken: "token")
        signedIn.handleIncomingURL(shareURL)
        settle()
        signedIn.testDismissPendingShareLink()
        settle()
        XCTAssertNil(signedIn.testPendingShareLinkCode)
        XCTAssertFalse(signedIn.testViewControllers.contains { $0 is ShareLinkLandingViewController })
    }

    // MARK: - The Avatar gate ( close-out)

    func test_returningVisitorWithAnAvatar_entersDirectly() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(shareURL)
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        settle()

        coordinator.testContinueVisitorEntry(code: "abcdefghijklmnopqrstuv", avatarID: "avatar_3")
        settle()

        XCTAssertFalse(
            coordinator.testViewControllers.contains { $0 is AvatarSelectionViewController },
            "an account that already chose an Avatar must not be asked again"
        )
        topIs(coordinator, LobbyEntryViewController.self, "straight into the visit")
        XCTAssertNil(coordinator.testPendingShareLinkCode, "the link has been consumed")
    }

    func test_returningVisitorWithoutAnAvatar_choosesOneFirst() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(shareURL)
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        settle()

        coordinator.testContinueVisitorEntry(code: "abcdefghijklmnopqrstuv", avatarID: "")

        topIs(coordinator, AvatarSelectionViewController.self, "the existing Avatar Selection screen")
        XCTAssertFalse(
            coordinator.testViewControllers.contains { $0 is AccountCreationViewController },
            "onboarding must not be restarted — this account already exists"
        )
        XCTAssertFalse(
            coordinator.testViewControllers.contains { $0 is LobbyEntryViewController },
            "the visitor experience must not be entered yet"
        )
    }

    func test_pendingShareLinkSurvivesAvatarSelection() {
        let coordinator = makeCoordinator()
        coordinator.handleIncomingURL(shareURL)
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        settle()

        coordinator.testContinueVisitorEntry(code: "abcdefghijklmnopqrstuv", avatarID: "   ")
        settle()

        topIs(coordinator, AvatarSelectionViewController.self)
        XCTAssertEqual(
            coordinator.testPendingShareLinkCode, "abcdefghijklmnopqrstuv",
            "the link must still be pending while the Avatar is chosen"
        )

        let avatar = coordinator.testViewControllers.compactMap { $0 as? AvatarSelectionViewController }.last
        avatar?.testCompleteSelection(avatarID: "avatar_2")
        settle()

        topIs(coordinator, LobbyEntryViewController.self, "back into the same visitor entry flow")
        XCTAssertNil(coordinator.testPendingShareLinkCode)
    }

    func test_withoutAPendingLink_theAvatarGateIsNeverReached() {
        let coordinator = makeCoordinator()
        coordinator.testCompleteLaunchRouting(accessToken: "token")
        settle()

        topIs(coordinator, MainProductChoiceViewController.self)
        XCTAssertNil(coordinator.testPendingShareLinkCode)
        XCTAssertFalse(coordinator.testViewControllers.contains { $0 is AvatarSelectionViewController })

        coordinator.testCompleteAuthentication(loginResult(isNewAccount: false))
        settle()
        topIs(coordinator, MainProductChoiceViewController.self)
        XCTAssertFalse(coordinator.testViewControllers.contains { $0 is AvatarSelectionViewController })
    }
}

final class VisitorAvatarRuleTests: XCTestCase {
    func test_requiresAvatarSelection_onlyWhenNothingIsSelected() {
        XCTAssertTrue(DeepLinkRouting.requiresAvatarSelection(avatarID: ""))
        XCTAssertTrue(DeepLinkRouting.requiresAvatarSelection(avatarID: nil))
        XCTAssertTrue(DeepLinkRouting.requiresAvatarSelection(avatarID: "   \n"))
        XCTAssertFalse(DeepLinkRouting.requiresAvatarSelection(avatarID: "avatar_1"))
        for avatar in AvatarCatalog.all {
            XCTAssertFalse(DeepLinkRouting.requiresAvatarSelection(avatarID: avatar.id))
        }
    }
}
