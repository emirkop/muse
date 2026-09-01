import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class EmailAuthViewModelTests: XCTestCase {
    private func harness() -> (EmailAuthViewModel, FakeAuthenticationService, FakeSessionStore) {
        let service = FakeAuthenticationService()
        let store = FakeSessionStore()
        return (EmailAuthViewModel(authService: service, sessionStore: store), service, store)
    }

    // MARK: -: verify-first

    func test_signUpProducesNoSessionAndSavesNothing() async {
        let (viewModel, service, store) = harness()

        await viewModel.signUp(email: "new@example.com", password: "a-good-passphrase", confirmation: "a-good-passphrase")

        guard case .acknowledged = viewModel.state else {
            return XCTFail("expected an acknowledgement, got \(viewModel.state)")
        }
        XCTAssertEqual(service.signUpCalls.count, 1)
        XCTAssertNil(store.savedSession, "sign-up must not produce or store a session")
    }

    func test_signUpNormalisesTheAddressBeforeSending() async {
        let (viewModel, service, _) = harness()

        await viewModel.signUp(email: "  New@Example.COM ", password: "a-good-passphrase", confirmation: "a-good-passphrase")

        XCTAssertEqual(service.signUpCalls.first?.email, "new@example.com")
    }

    func test_signUpValidatesLocallyBeforeCallingTheServer() async {
        let (viewModel, service, _) = harness()

        await viewModel.signUp(email: "nope", password: "a-good-passphrase", confirmation: "a-good-passphrase")
        await viewModel.signUp(email: "ok@example.com", password: "short", confirmation: "short")
        await viewModel.signUp(email: "ok@example.com", password: "a-good-passphrase", confirmation: "mismatch")

        XCTAssertTrue(service.signUpCalls.isEmpty, "invalid input must not reach the network")
        guard case .failed = viewModel.state else {
            return XCTFail("expected a failure state, got \(viewModel.state)")
        }
    }

    func test_verificationProducesAndStoresTheSession() async {
        let (viewModel, service, store) = harness()

        await viewModel.verify(token: "  the-token  ")

        guard case .authenticated(let result) = viewModel.state else {
            return XCTFail("expected authentication, got \(viewModel.state)")
        }
        XCTAssertTrue(result.isNewAccount, "the account is created at verification")
        XCTAssertEqual(service.verifyTokens, ["the-token"], "the token must be trimmed before sending")
        XCTAssertNotNil(store.savedSession, "the session from verification must be persisted")
    }

    func test_verificationRefusesAnEmptyToken() async {
        let (viewModel, service, _) = harness()

        await viewModel.verify(token: "   ")

        XCTAssertTrue(service.verifyTokens.isEmpty)
        guard case .failed = viewModel.state else {
            return XCTFail("expected a failure, got \(viewModel.state)")
        }
    }

    // MARK: - Log in

    func test_logInStoresTheSession() async {
        let (viewModel, service, store) = harness()

        await viewModel.logIn(email: "User@Example.com", password: "the-passphrase")

        guard case .authenticated = viewModel.state else {
            return XCTFail("expected authentication, got \(viewModel.state)")
        }
        XCTAssertEqual(service.emailLoginCalls.first?.email, "user@example.com")
        XCTAssertEqual(service.emailLoginCalls.first?.password, "the-passphrase")
        XCTAssertNotNil(store.savedSession)
    }

    // MARK: - Enumeration resistance in the copy

    func test_signUpCopyIsIdenticalForAnyServerOutcome(  ) async {
        let (viewModel, _, _) = harness()

        await viewModel.signUp(email: "first@example.com", password: "a-good-passphrase", confirmation: "a-good-passphrase")
        guard case .acknowledged(let firstMessage) = viewModel.state else {
            return XCTFail("expected acknowledgement")
        }
        await viewModel.signUp(email: "second@example.com", password: "a-good-passphrase", confirmation: "a-good-passphrase")
        guard case .acknowledged(let secondMessage) = viewModel.state else {
            return XCTFail("expected acknowledgement")
        }

        XCTAssertEqual(firstMessage, secondMessage)
    }

    func test_resetRequestCopyIsNeutral() async {
        let (viewModel, _, _) = harness()

        await viewModel.requestPasswordReset(email: "anyone@example.com")

        guard case .acknowledged(let message) = viewModel.state else {
            return XCTFail("expected an acknowledgement, got \(viewModel.state)")
        }
        XCTAssertTrue(
            message.lowercased().contains("if an account exists"),
            "the copy must not confirm or deny that the address is registered: \(message)"
        )
    }

    func test_a401RendersAsOneGenericMessage() {
        let message = EmailAuthViewModel.message(
            for: IdentityAPIClientError.server(statusCode: 401, message: "email or password is incorrect")
        )

        XCTAssertEqual(message, "Email or password is incorrect.")
        XCTAssertFalse(message.lowercased().contains("no account"))
        XCTAssertFalse(message.lowercased().contains("not found"))
    }

    func test_throttlingIsSurfacedPlainly() {
        let message = EmailAuthViewModel.message(
            for: IdentityAPIClientError.server(statusCode: 429, message: "too many attempts, please try again later")
        )

        XCTAssertTrue(message.lowercased().contains("too many attempts"), "a user must know to wait: \(message)")
    }

    func test_a400SurfacesTheServersGuidance() {
        let message = EmailAuthViewModel.message(
            for: IdentityAPIClientError.server(statusCode: 400, message: "choose a password between 10 and 128 characters")
        )

        XCTAssertTrue(message.contains("10"))
    }

    func test_a503PointsAtAppleAndGoogle() {
        let message = EmailAuthViewModel.message(for: IdentityAPIClientError.server(statusCode: 503, message: nil))

        XCTAssertTrue(message.contains("Apple"))
        XCTAssertTrue(message.contains("Google"))
    }

    func test_transportFailureIsDistinctFromARejection() {
        let message = EmailAuthViewModel.message(for: IdentityAPIClientError.transport)

        XCTAssertTrue(message.lowercased().contains("connection"), "a network failure is not a credential failure: \(message)")
    }

    // MARK: - Password reset

    func test_resetConfirmSaysThatEverySessionIsGone() async {
        let (viewModel, service, store) = harness()

        await viewModel.confirmPasswordReset(
            token: "the-token", password: "a-brand-new-passphrase", confirmation: "a-brand-new-passphrase"
        )

        guard case .acknowledged(let message) = viewModel.state else {
            return XCTFail("expected an acknowledgement, got \(viewModel.state)")
        }
        XCTAssertTrue(
            message.lowercased().contains("signed out"),
            "the reset revokes every session; the user must be told why they have to log in again: \(message)"
        )
        XCTAssertEqual(service.resetConfirmCalls.first?.token, "the-token")
        XCTAssertNil(store.savedSession, "a reset must not hand out a session")
    }

    func test_resetConfirmValidatesLocally() async {
        let (viewModel, service, _) = harness()

        await viewModel.confirmPasswordReset(token: "", password: "a-good-passphrase", confirmation: "a-good-passphrase")
        await viewModel.confirmPasswordReset(token: "t", password: "short", confirmation: "short")
        await viewModel.confirmPasswordReset(token: "t", password: "a-good-passphrase", confirmation: "different")

        XCTAssertTrue(service.resetConfirmCalls.isEmpty)
    }

    func test_resendReplacesThePreviousLinkInItsCopy() async {
        let (viewModel, service, _) = harness()

        await viewModel.resendVerification(email: "Pending@Example.com")

        XCTAssertEqual(service.resendEmails, ["pending@example.com"])
        guard case .acknowledged(let message) = viewModel.state else {
            return XCTFail("expected an acknowledgement")
        }
        XCTAssertTrue(
            message.lowercased().contains("replaces"),
            "a resend invalidates the previous link; the copy must say so: \(message)"
        )
    }
}

@MainActor
final class CredentialScreenTests: XCTestCase {
    private func providerViewModel() -> AuthenticationViewModel {
        AuthenticationViewModel(
            appleSignInUseCase: SignInWithAppleUseCase(
                identityProvider: FakeAppleIdentityProvider(),
                authService: FakeAuthenticationService(),
                sessionStore: FakeSessionStore()
            ),
            googleSignInUseCase: SignInWithGoogleUseCase(
                identityProvider: FakeGoogleIdentityProvider(),
                authService: FakeAuthenticationService(),
                sessionStore: FakeSessionStore()
            )
        )
    }

    private func makeLogIn(service: FakeAuthenticationService = FakeAuthenticationService())
        -> (LogInViewController, FakeAuthenticationService, FakeSessionStore) {
        let store = FakeSessionStore()
        let controller = LogInViewController(
            emailViewModel: EmailAuthViewModel(authService: service, sessionStore: store),
            providerViewModel: providerViewModel(),
            onSignedIn: { _ in },
            onSignUp: {},
            onForgotPassword: { _ in }
        )
        controller.loadViewIfNeeded()
        return (controller, service, store)
    }

    private func makeSignUp(service: FakeAuthenticationService = FakeAuthenticationService())
        -> (SignUpViewController, FakeAuthenticationService) {
        let controller = SignUpViewController(
            emailViewModel: EmailAuthViewModel(authService: service, sessionStore: FakeSessionStore()),
            providerViewModel: providerViewModel(),
            onSignedIn: { _ in },
            onVerificationSent: { _ in },
            onLogIn: {}
        )
        controller.loadViewIfNeeded()
        return (controller, service)
    }

    private func labels(in view: UIView) -> [String] {
        var found: [String] = []
        for subview in view.subviews {
            if let label = subview as? UILabel, let text = label.text { found.append(text) }
            if let button = subview as? UIButton, let title = button.configuration?.title { found.append(title) }
            found.append(contentsOf: labels(in: subview))
        }
        return found
    }

    // MARK: - Contents, per `02`

    func test_logInScreenCarriesEveryElementO2Specifies() {
        let (controller, _, _) = makeLogIn()

        let text = labels(in: controller.view).joined(separator: " | ")

        for expected in ["Forgot Password?", "Log In", "or", "Continue with Apple", "Continue with Google"] {
            XCTAssertTrue(text.contains(expected), "Log In must offer '\(expected)'. Found: \(text)")
        }
        XCTAssertTrue(text.contains("Sign Up"), "Log In must link across to Sign Up")
    }

    func test_signUpScreenCarriesEveryElementO2Specifies() {
        let (controller, _) = makeSignUp()

        let text = labels(in: controller.view).joined(separator: " | ")

        for expected in ["Create Account", "or", "Continue with Apple", "Continue with Google"] {
            XCTAssertTrue(text.contains(expected), "Sign Up must offer '\(expected)'. Found: \(text)")
        }
        XCTAssertTrue(text.contains("Log In"), "Sign Up must link across to Log In")
    }

    func test_providerButtonsAppearInO2sOrder() {
        let (controller, _, _) = makeLogIn()

        let text = labels(in: controller.view)
        guard let apple = text.firstIndex(of: "Continue with Apple"),
              let google = text.firstIndex(of: "Continue with Google") else {
            return XCTFail("both provider buttons must be present")
        }
        XCTAssertLessThan(apple, google, "Apple precedes Google")
    }

    // MARK: - Password field hygiene

    func test_passwordFieldsAreSecure() {
        let (logIn, _, _) = makeLogIn()
        let (signUp, _) = makeSignUp()

        XCTAssertTrue(logIn.testPasswordFieldIsSecure)
        XCTAssertTrue(signUp.testPasswordFieldsAreSecure)
    }

    // MARK: - Enablement

    func test_logInIsDisabledUntilBothFieldsAreUsable() {
        let (controller, _, _) = makeLogIn()

        XCTAssertFalse(controller.testLogInEnabled, "an empty form must not be submittable")

        controller.testSetCredentials(email: "nope", password: "x")
        XCTAssertFalse(controller.testLogInEnabled)

        controller.testSetCredentials(email: "someone@example.com", password: "x")
        XCTAssertTrue(controller.testLogInEnabled)
    }

    func test_createAccountIsDisabledUntilTheFormIsValid() {
        let (controller, _) = makeSignUp()

        XCTAssertFalse(controller.testCreateEnabled)

        controller.testSetCredentials(email: "a@b.com", password: "a-good-passphrase", confirmation: "different")
        XCTAssertFalse(controller.testCreateEnabled, "a mismatch must block submission")

        controller.testSetCredentials(email: "a@b.com", password: "a-good-passphrase", confirmation: "a-good-passphrase")
        XCTAssertTrue(controller.testCreateEnabled)
    }

    func test_mismatchIsShownWhileTyping() {
        let (controller, _) = makeSignUp()

        controller.testSetCredentials(email: "a@b.com", password: "a-good-passphrase", confirmation: "a-good")

        XCTAssertEqual(controller.testStatusText, EmailCredentialRules.Problem.passwordsDoNotMatch.message)
    }

    // MARK: - Wiring

    func test_logInSendsTheNormalisedAddress() async {
        let service = FakeAuthenticationService()
        let (controller, _, _) = makeLogIn(service: service)
        controller.testSetCredentials(email: " User@Example.COM ", password: "the-passphrase")

        controller.testTapLogIn()
        await waitFor { service.emailLoginCalls.isEmpty == false }

        XCTAssertEqual(service.emailLoginCalls.first?.email, "user@example.com")
    }

    func test_signUpHandsOffToVerificationWithoutASession() async {
        let service = FakeAuthenticationService()
        var verificationEmail: String?
        var signedIn = false
        let controller = SignUpViewController(
            emailViewModel: EmailAuthViewModel(authService: service, sessionStore: FakeSessionStore()),
            providerViewModel: providerViewModel(),
            onSignedIn: { _ in signedIn = true },
            onVerificationSent: { verificationEmail = $0 },
            onLogIn: {}
        )
        controller.loadViewIfNeeded()
        controller.testSetCredentials(
            email: "New@Example.com", password: "a-good-passphrase", confirmation: "a-good-passphrase"
        )

        controller.testTapCreate()
        await waitFor { verificationEmail != nil }

        XCTAssertEqual(verificationEmail, "new@example.com")
        XCTAssertFalse(signedIn, "creating an account must not sign anyone in")
    }

    func test_logInShowsOneGenericMessageForARejection() async {
        let service = FakeAuthenticationService()
        service.emailLoginResult = .failure(IdentityAPIClientError.server(statusCode: 401, message: "email or password is incorrect"))
        let (controller, _, _) = makeLogIn(service: service)
        controller.testSetCredentials(email: "someone@example.com", password: "wrong")

        controller.testTapLogIn()
        await waitFor { controller.testStatusText != nil }

        XCTAssertEqual(controller.testStatusText, "Email or password is incorrect.")
    }

    private func waitFor(_ condition: @escaping () -> Bool, attempts: Int = 200) async {
        for _ in 0..<attempts {
            if condition() { return }
            await Task.yield()
        }
    }
}

@MainActor
final class VerificationAndResetScreenTests: XCTestCase {

    func test_verificationScreenSaysNothingIsSavedYet() {
        let controller = VerificationPendingViewController(
            viewModel: EmailAuthViewModel(authService: FakeAuthenticationService(), sessionStore: FakeSessionStore()),
            email: "new@example.com",
            onVerified: { _ in },
            onBackToLogIn: {}
        )
        controller.loadViewIfNeeded()

        let text = allText(in: controller.view)
        XCTAssertTrue(text.contains("new@example.com"), "the address must be echoed so a typo is catchable")
        XCTAssertTrue(
            text.lowercased().contains("isn't created"),
            "the screen must say the account does not exist yet. Found: \(text)"
        )
    }

    func test_verifyIsDisabledUntilATokenIsEntered() {
        let controller = VerificationPendingViewController(
            viewModel: EmailAuthViewModel(authService: FakeAuthenticationService(), sessionStore: FakeSessionStore()),
            email: "new@example.com",
            onVerified: { _ in },
            onBackToLogIn: {}
        )
        controller.loadViewIfNeeded()

        XCTAssertFalse(controller.testVerifyEnabled)
        controller.testSetToken("   ")
        XCTAssertFalse(controller.testVerifyEnabled, "whitespace is not a token")
        controller.testSetToken("a-token")
        XCTAssertTrue(controller.testVerifyEnabled)
    }

    func test_verificationHandsBackASession() async {
        let service = FakeAuthenticationService()
        var received: LoginResult?
        let controller = VerificationPendingViewController(
            viewModel: EmailAuthViewModel(authService: service, sessionStore: FakeSessionStore()),
            email: "new@example.com",
            onVerified: { received = $0 },
            onBackToLogIn: {}
        )
        controller.loadViewIfNeeded()
        controller.testSetToken("the-token")

        controller.testTapVerify()
        for _ in 0..<200 where received == nil { await Task.yield() }

        XCTAssertNotNil(received, "verification is where the session comes from")
    }

    func test_resetScreenStartsByAskingForAnAddress() {
        let controller = PasswordResetViewController(
            viewModel: EmailAuthViewModel(authService: FakeAuthenticationService(), sessionStore: FakeSessionStore()),
            onFinished: {}
        )
        controller.loadViewIfNeeded()

        XCTAssertEqual(controller.testPrimaryTitle, "Send Reset Link")
        XCTAssertFalse(controller.testIsOnPasswordStep)
        XCTAssertFalse(controller.testPrimaryEnabled, "no address typed yet")

        controller.testSetEmail("someone@example.com")
        XCTAssertTrue(controller.testPrimaryEnabled)
    }

    func test_resetRequestShowsTheNeutralConfirmationAndAdvances() async {
        let service = FakeAuthenticationService()
        let controller = PasswordResetViewController(
            viewModel: EmailAuthViewModel(authService: service, sessionStore: FakeSessionStore()),
            onFinished: {}
        )
        controller.loadViewIfNeeded()
        controller.testSetEmail("someone@example.com")

        controller.testTapPrimary()
        for _ in 0..<200 where !controller.testIsOnPasswordStep { await Task.yield() }

        XCTAssertTrue(controller.testIsOnPasswordStep)
        let message = controller.testStatusText ?? ""
        XCTAssertTrue(
            message.lowercased().contains("if an account exists"),
            "the confirmation must be neutral: \(message)"
        )
    }

    func test_resetConfirmRequiresATokenAndAMatchingPassword() {
        let controller = PasswordResetViewController(
            viewModel: EmailAuthViewModel(authService: FakeAuthenticationService(), sessionStore: FakeSessionStore()),
            onFinished: {}
        )
        controller.loadViewIfNeeded()
        controller.testTapHaveCode()
        XCTAssertTrue(controller.testIsOnPasswordStep)
        XCTAssertEqual(controller.testPrimaryTitle, "Reset Password")

        controller.testSetReset(token: "", password: "a-good-passphrase", confirmation: "a-good-passphrase")
        XCTAssertFalse(controller.testPrimaryEnabled, "a token is required")

        controller.testSetReset(token: "t", password: "a-good-passphrase", confirmation: "different")
        XCTAssertFalse(controller.testPrimaryEnabled, "the two passwords must match")

        controller.testSetReset(token: "t", password: "a-good-passphrase", confirmation: "a-good-passphrase")
        XCTAssertTrue(controller.testPrimaryEnabled)
    }

    private func allText(in view: UIView) -> String {
        var found: [String] = []
        for subview in view.subviews {
            if let label = subview as? UILabel, let text = label.text { found.append(text) }
            if let button = subview as? UIButton, let title = button.configuration?.title { found.append(title) }
            found.append(allText(in: subview))
        }
        return found.joined(separator: " | ")
    }
}
