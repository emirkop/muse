import UIKit

final class PasswordResetViewController: UIViewController {
    private enum Step {
        case requestLink
        case chooseNewPassword
    }

    private let viewModel: EmailAuthViewModel
    private let onFinished: () -> Void

    private var step: Step = .requestLink

    private let emailField = CredentialScreenViews.makeField(
        placeholder: "Email", secure: false, contentType: .username
    )
    private let tokenField = CredentialScreenViews.makeField(
        placeholder: "Paste the code from your email", secure: false, contentType: .oneTimeCode
    )
    private let passwordField = CredentialScreenViews.makeField(
        placeholder: "New Password", secure: true, contentType: .newPassword
    )
    private let confirmField = CredentialScreenViews.makeField(
        placeholder: "Confirm New Password", secure: true, contentType: .newPassword
    )
    private let primaryButton = CredentialScreenViews.makePrimaryButton(title: "Send Reset Link")
    private let haveCodeButton = CredentialScreenViews.makeLinkButton(title: "I already have a code")
    private let backButton = CredentialScreenViews.makeLinkButton(title: "Back to Log In")
    private let titleLabel = UILabel()
    private let bodyLabel = CredentialScreenViews.makeStatusLabel()
    private let statusLabel = CredentialScreenViews.makeStatusLabel()
    private let activityIndicator = UIActivityIndicatorView(style: .medium)

    init(viewModel: EmailAuthViewModel, prefilledEmail: String = "", onFinished: @escaping () -> Void) {
        self.viewModel = viewModel
        self.onFinished = onFinished
        super.init(nibName: nil, bundle: nil)
        emailField.text = prefilledEmail
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Reset Password"
        view.backgroundColor = .systemBackground
        configureLayout()

        primaryButton.addTarget(self, action: #selector(handlePrimary), for: .touchUpInside)
        haveCodeButton.addTarget(self, action: #selector(handleHaveCode), for: .touchUpInside)
        backButton.addTarget(self, action: #selector(handleBack), for: .touchUpInside)
        for field in [emailField, tokenField, passwordField, confirmField] {
            field.addTarget(self, action: #selector(handleFieldChanged), for: .editingChanged)
        }

        viewModel.onStateChange = { [weak self] state in self?.render(state) }
        renderStep()
    }

    private func configureLayout() {
        titleLabel.museMarkAsHeader()
        titleLabel.font = .preferredFont(forTextStyle: .title2)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0

        let stack = UIStackView(arrangedSubviews: [
            titleLabel,
            bodyLabel,
            emailField,
            tokenField,
            passwordField,
            confirmField,
            primaryButton,
            activityIndicator,
            statusLabel,
            haveCodeButton,
            backButton
        ])
        stack.axis = .vertical
        stack.spacing = 14
        stack.setCustomSpacing(22, after: bodyLabel)
        CredentialScreenViews.embedScrolling(stack, in: view)

        NSLayoutConstraint.activate([
            primaryButton.heightAnchor.constraint(greaterThanOrEqualToConstant: 46)
        ])
    }

    // MARK: - Step rendering

    private func renderStep() {
        switch step {
        case .requestLink:
            titleLabel.text = "Reset your password"
            bodyLabel.text = "Enter your email address and we'll send you a link to choose a new password."
            emailField.isHidden = false
            tokenField.isHidden = true
            passwordField.isHidden = true
            confirmField.isHidden = true
            haveCodeButton.isHidden = false
            primaryButton.configuration?.title = "Send Reset Link"
        case .chooseNewPassword:
            titleLabel.text = "Choose a new password"
            bodyLabel.text = "Paste the code from your email, then pick a new password. "
                + "Resetting signs you out on every device."
            emailField.isHidden = true
            tokenField.isHidden = false
            passwordField.isHidden = false
            confirmField.isHidden = false
            haveCodeButton.isHidden = true
            primaryButton.configuration?.title = "Reset Password"
        }
        handleFieldChanged()
    }

    // MARK: - Actions

    @objc private func handleFieldChanged() {
        switch step {
        case .requestLink:
            primaryButton.isEnabled = EmailCredentialRules.isPlausibleEmail(emailField.text ?? "")
        case .chooseNewPassword:
            let hasToken = !(tokenField.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            primaryButton.isEnabled = hasToken && EmailCredentialRules.resetProblem(
                password: passwordField.text ?? "",
                confirmation: confirmField.text ?? ""
            ) == nil
        }
    }

    @objc private func handlePrimary() {
        view.endEditing(true)
        switch step {
        case .requestLink:
            let email = emailField.text ?? ""
            Task { await viewModel.requestPasswordReset(email: email) }
        case .chooseNewPassword:
            let token = tokenField.text ?? ""
            let password = passwordField.text ?? ""
            let confirmation = confirmField.text ?? ""
            Task {
                await viewModel.confirmPasswordReset(token: token, password: password, confirmation: confirmation)
            }
        }
    }

    @objc private func handleHaveCode() {
        step = .chooseNewPassword
        renderStep()
    }

    @objc private func handleBack() {
        onFinished()
    }

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
            switch step {
            case .requestLink:
                step = .chooseNewPassword
                renderStep()
                statusLabel.text = message
                MuseAccessibility.announceScreenChange(focusing: titleLabel)
            case .chooseNewPassword:
                primaryButton.isEnabled = false
                backButton.configuration?.title = "Log In"
            }
        case .authenticated:
            setBusy(false)
        }
    }

    private func setBusy(_ busy: Bool) {
        if busy { activityIndicator.startAnimating() } else { activityIndicator.stopAnimating() }
        primaryButton.isEnabled = !busy
        haveCodeButton.isEnabled = !busy
        backButton.isEnabled = !busy
        if !busy { handleFieldChanged() }
    }

    // MARK: - Test seam

    func testSetEmail(_ email: String) {
        emailField.text = email
        handleFieldChanged()
    }
    func testSetReset(token: String, password: String, confirmation: String) {
        tokenField.text = token
        passwordField.text = password
        confirmField.text = confirmation
        handleFieldChanged()
    }
    func testTapPrimary() { handlePrimary() }
    func testTapHaveCode() { handleHaveCode() }
    var testPrimaryTitle: String? { primaryButton.configuration?.title }
    var testPrimaryEnabled: Bool { primaryButton.isEnabled }
    var testStatusText: String? { statusLabel.text }
    var testIsOnPasswordStep: Bool { step == .chooseNewPassword }
}
