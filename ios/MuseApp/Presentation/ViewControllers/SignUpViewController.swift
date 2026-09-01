import UIKit

final class SignUpViewController: UIViewController {
    private let emailViewModel: EmailAuthViewModel
    private let providerViewModel: AuthenticationViewModel
    private let onSignedIn: (LoginResult) -> Void
    private let onVerificationSent: (String) -> Void
    private let onLogIn: () -> Void

    private let emailField = CredentialScreenViews.makeField(
        placeholder: "Email", secure: false, contentType: .username
    )
    private let passwordField = CredentialScreenViews.makeField(
        placeholder: "Password", secure: true, contentType: .newPassword
    )
    private let confirmField = CredentialScreenViews.makeField(
        placeholder: "Confirm Password", secure: true, contentType: .newPassword
    )
    private let createButton = CredentialScreenViews.makePrimaryButton(title: "Create Account")
    private let appleButton = CredentialScreenViews.makeProviderButton(title: "Continue with Apple")
    private let googleButton = CredentialScreenViews.makeProviderButton(title: "Continue with Google")
    private let logInButton = CredentialScreenViews.makeLinkButton(title: "Already have an account? Log In")
    private let requirementLabel = CredentialScreenViews.makeStatusLabel()
    private let statusLabel = CredentialScreenViews.makeStatusLabel()
    private let activityIndicator = UIActivityIndicatorView(style: .medium)

    var enteredEmail: String { emailField.text ?? "" }

    init(
        emailViewModel: EmailAuthViewModel,
        providerViewModel: AuthenticationViewModel,
        prefilledEmail: String = "",
        onSignedIn: @escaping (LoginResult) -> Void,
        onVerificationSent: @escaping (String) -> Void,
        onLogIn: @escaping () -> Void
    ) {
        self.emailViewModel = emailViewModel
        self.providerViewModel = providerViewModel
        self.onSignedIn = onSignedIn
        self.onVerificationSent = onVerificationSent
        self.onLogIn = onLogIn
        super.init(nibName: nil, bundle: nil)
        emailField.text = prefilledEmail
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Sign Up"
        view.backgroundColor = .systemBackground
        configureLayout()

        createButton.addTarget(self, action: #selector(handleCreate), for: .touchUpInside)
        logInButton.addTarget(self, action: #selector(handleLogIn), for: .touchUpInside)
        appleButton.addTarget(self, action: #selector(handleApple), for: .touchUpInside)
        googleButton.addTarget(self, action: #selector(handleGoogle), for: .touchUpInside)
        for field in [emailField, passwordField, confirmField] {
            field.addTarget(self, action: #selector(handleFieldChanged), for: .editingChanged)
        }
        confirmField.returnKeyType = .go
        confirmField.addTarget(self, action: #selector(handleCreate), for: .primaryActionTriggered)

        emailViewModel.onStateChange = { [weak self] state in self?.render(state) }
        providerViewModel.onStateChange = { [weak self] state in self?.renderProvider(state) }
        handleFieldChanged()
    }

    private func configureLayout() {
        let titleLabel = UILabel()
        titleLabel.text = "Create your Muse account"
        titleLabel.font = .preferredFont(forTextStyle: .title2)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        requirementLabel.text = "Passwords need at least \(EmailCredentialRules.passwordMinimumLength) characters."

        let stack = UIStackView(arrangedSubviews: [
            titleLabel,
            emailField,
            passwordField,
            confirmField,
            requirementLabel,
            createButton,
            CredentialScreenViews.makeSeparator(),
            appleButton,
            googleButton,
            activityIndicator,
            statusLabel,
            logInButton
        ])
        stack.axis = .vertical
        stack.spacing = 14
        stack.setCustomSpacing(6, after: confirmField)
        stack.setCustomSpacing(22, after: createButton)
        CredentialScreenViews.embedScrolling(stack, in: view)

        CredentialScreenViews.pinActionHeights([createButton, appleButton, googleButton])
    }

    // MARK: - Actions

    @objc private func handleFieldChanged() {
        createButton.isEnabled = EmailCredentialRules.canAttemptSignUp(
            email: emailField.text ?? "",
            password: passwordField.text ?? "",
            confirmation: confirmField.text ?? ""
        )
        let password = passwordField.text ?? ""
        let confirmation = confirmField.text ?? ""
        if !confirmation.isEmpty, password != confirmation {
            statusLabel.textColor = .systemRed
            statusLabel.text = EmailCredentialRules.Problem.passwordsDoNotMatch.message
        } else if case .failed = emailViewModel.state {
        } else {
            statusLabel.text = nil
        }
    }

    @objc private func handleCreate() {
        view.endEditing(true)
        let email = emailField.text ?? ""
        let password = passwordField.text ?? ""
        let confirmation = confirmField.text ?? ""
        Task { await emailViewModel.signUp(email: email, password: password, confirmation: confirmation) }
    }

    @objc private func handleLogIn() {
        onLogIn()
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
        case .acknowledged:
            setBusy(true)
            onVerificationSent(EmailCredentialRules.normalised(emailField.text ?? ""))
        case .authenticated(let result):
            setBusy(true)
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
        createButton.isEnabled = !busy && EmailCredentialRules.canAttemptSignUp(
            email: emailField.text ?? "",
            password: passwordField.text ?? "",
            confirmation: confirmField.text ?? ""
        )
        appleButton.isEnabled = !busy
        googleButton.isEnabled = !busy
        logInButton.isEnabled = !busy
    }

    // MARK: - Test seam

    func testSetCredentials(email: String, password: String, confirmation: String) {
        emailField.text = email
        passwordField.text = password
        confirmField.text = confirmation
        handleFieldChanged()
    }
    func testTapCreate() { handleCreate() }
    var testCreateEnabled: Bool { createButton.isEnabled }
    var testStatusText: String? { statusLabel.text }
    var testPasswordFieldsAreSecure: Bool { passwordField.isSecureTextEntry && confirmField.isSecureTextEntry }
}
