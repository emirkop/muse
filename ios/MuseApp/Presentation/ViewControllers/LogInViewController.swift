import UIKit

final class LogInViewController: UIViewController {
    private let emailViewModel: EmailAuthViewModel
    private let providerViewModel: AuthenticationViewModel
    private let onSignedIn: (LoginResult) -> Void
    private let onSignUp: () -> Void
    private let onForgotPassword: (String) -> Void

    private let emailField = CredentialScreenViews.makeField(
        placeholder: "Email", secure: false, contentType: .username
    )
    private let passwordField = CredentialScreenViews.makeField(
        placeholder: "Password", secure: true, contentType: .password
    )
    private let forgotButton = CredentialScreenViews.makeLinkButton(title: "Forgot Password?")
    private let logInButton = CredentialScreenViews.makePrimaryButton(title: "Log In")
    private let appleButton = CredentialScreenViews.makeProviderButton(title: "Continue with Apple")
    private let googleButton = CredentialScreenViews.makeProviderButton(title: "Continue with Google")
    private let signUpButton = CredentialScreenViews.makeLinkButton(title: "Don't have an account? Sign Up")
    private let statusLabel = CredentialScreenViews.makeStatusLabel()
    private let activityIndicator = UIActivityIndicatorView(style: .medium)

    var enteredEmail: String { emailField.text ?? "" }

    init(
        emailViewModel: EmailAuthViewModel,
        providerViewModel: AuthenticationViewModel,
        prefilledEmail: String = "",
        onSignedIn: @escaping (LoginResult) -> Void,
        onSignUp: @escaping () -> Void,
        onForgotPassword: @escaping (String) -> Void
    ) {
        self.emailViewModel = emailViewModel
        self.providerViewModel = providerViewModel
        self.onSignedIn = onSignedIn
        self.onSignUp = onSignUp
        self.onForgotPassword = onForgotPassword
        super.init(nibName: nil, bundle: nil)
        emailField.text = prefilledEmail
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Log In"
        view.backgroundColor = .systemBackground
        configureLayout()

        logInButton.addTarget(self, action: #selector(handleLogIn), for: .touchUpInside)
        forgotButton.addTarget(self, action: #selector(handleForgot), for: .touchUpInside)
        signUpButton.addTarget(self, action: #selector(handleSignUp), for: .touchUpInside)
        appleButton.addTarget(self, action: #selector(handleApple), for: .touchUpInside)
        googleButton.addTarget(self, action: #selector(handleGoogle), for: .touchUpInside)
        emailField.addTarget(self, action: #selector(handleFieldChanged), for: .editingChanged)
        passwordField.addTarget(self, action: #selector(handleFieldChanged), for: .editingChanged)
        passwordField.returnKeyType = .go
        passwordField.addTarget(self, action: #selector(handleLogIn), for: .primaryActionTriggered)

        emailViewModel.onStateChange = { [weak self] state in self?.render(state) }
        providerViewModel.onStateChange = { [weak self] state in self?.renderProvider(state) }
        handleFieldChanged()
    }

    private func configureLayout() {
        let titleLabel = UILabel()
        titleLabel.text = "Log in to Muse"
        titleLabel.font = .preferredFont(forTextStyle: .title2)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        let forgotRow = UIStackView(arrangedSubviews: [UIView(), forgotButton])
        forgotRow.axis = .horizontal

        let stack = UIStackView(arrangedSubviews: [
            titleLabel,
            emailField,
            passwordField,
            forgotRow,
            logInButton,
            CredentialScreenViews.makeSeparator(),
            appleButton,
            googleButton,
            activityIndicator,
            statusLabel,
            signUpButton
        ])
        stack.axis = .vertical
        stack.spacing = 14
        stack.setCustomSpacing(6, after: passwordField)
        stack.setCustomSpacing(22, after: logInButton)
        CredentialScreenViews.embedScrolling(stack, in: view)

        CredentialScreenViews.pinActionHeights([logInButton, appleButton, googleButton])
    }

    // MARK: - Actions

    @objc private func handleFieldChanged() {
        logInButton.isEnabled = EmailCredentialRules.canAttemptLogIn(
            email: emailField.text ?? "",
            password: passwordField.text ?? ""
        )
    }

    @objc private func handleLogIn() {
        view.endEditing(true)
        let email = emailField.text ?? ""
        let password = passwordField.text ?? ""
        Task { await emailViewModel.logIn(email: email, password: password) }
    }

    @objc private func handleForgot() {
        onForgotPassword(emailField.text ?? "")
    }

    @objc private func handleSignUp() {
        onSignUp()
    }

    @objc private func handleApple() {
        Task { await providerViewModel.signInWithApple() }
    }

    @objc private func handleGoogle() {
        Task { await providerViewModel.signInWithGoogle() }
    }

    // MARK: - Rendering

    private func showStatus(_ message: String?, isFailure: Bool) {
        statusLabel.textColor = isFailure ? .systemRed : .secondaryLabel
        statusLabel.text = message
        if isFailure {
            MuseAccessibility.announceFailure(message)
        } else {
            MuseAccessibility.announce(message)
        }
    }

    private func render(_ state: EmailAuthViewModel.State) {
        switch state {
        case .idle:
            setBusy(false)
            statusLabel.text = nil
        case .working:
            setBusy(true)
            statusLabel.text = nil
        case .failed(let message):
            setBusy(false)
            showStatus(message, isFailure: true)
        case .acknowledged(let message):
            setBusy(false)
            showStatus(message, isFailure: false)
        case .authenticated(let result):
            setBusy(true)
            statusLabel.text = nil
            onSignedIn(result)
        }
    }

    private func renderProvider(_ state: AuthenticationViewModel.State) {
        switch state {
        case .idle:
            setBusy(false)
            statusLabel.text = nil
        case .loading:
            setBusy(true)
        case .failed(let message):
            setBusy(false)
            showStatus(message, isFailure: true)
        case .succeeded(let result):
            setBusy(true)
            onSignedIn(result)
        }
    }

    private func setBusy(_ busy: Bool) {
        if busy { activityIndicator.startAnimating() } else { activityIndicator.stopAnimating() }
        logInButton.isEnabled = !busy && EmailCredentialRules.canAttemptLogIn(
            email: emailField.text ?? "", password: passwordField.text ?? ""
        )
        appleButton.isEnabled = !busy
        googleButton.isEnabled = !busy
        signUpButton.isEnabled = !busy
        forgotButton.isEnabled = !busy
    }

    // MARK: - Test seam

    func testTapLogIn() { handleLogIn() }
    func testSetCredentials(email: String, password: String) {
        emailField.text = email
        passwordField.text = password
        handleFieldChanged()
    }
    var testLogInEnabled: Bool { logInButton.isEnabled }
    var testStatusText: String? { statusLabel.text }
    var testPasswordFieldIsSecure: Bool { passwordField.isSecureTextEntry }
}
