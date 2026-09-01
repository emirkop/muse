import UIKit

final class VerificationPendingViewController: UIViewController {
    private let viewModel: EmailAuthViewModel
    private let email: String
    private let onVerified: (LoginResult) -> Void
    private let onBackToLogIn: () -> Void

    private let tokenField = CredentialScreenViews.makeField(
        placeholder: "Paste the code from your email", secure: false, contentType: .oneTimeCode
    )
    private let verifyButton = CredentialScreenViews.makePrimaryButton(title: "Verify Email")
    private let resendButton = CredentialScreenViews.makeLinkButton(title: "Resend Email")
    private let backButton = CredentialScreenViews.makeLinkButton(title: "Back to Log In")
    private let statusLabel = CredentialScreenViews.makeStatusLabel()
    private let activityIndicator = UIActivityIndicatorView(style: .medium)

    init(
        viewModel: EmailAuthViewModel,
        email: String,
        onVerified: @escaping (LoginResult) -> Void,
        onBackToLogIn: @escaping () -> Void
    ) {
        self.viewModel = viewModel
        self.email = email
        self.onVerified = onVerified
        self.onBackToLogIn = onBackToLogIn
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Verify Email"
        view.backgroundColor = .systemBackground
        configureLayout()

        verifyButton.addTarget(self, action: #selector(handleVerify), for: .touchUpInside)
        resendButton.addTarget(self, action: #selector(handleResend), for: .touchUpInside)
        backButton.addTarget(self, action: #selector(handleBack), for: .touchUpInside)
        tokenField.addTarget(self, action: #selector(handleFieldChanged), for: .editingChanged)
        tokenField.keyboardType = .default
        tokenField.returnKeyType = .go
        tokenField.addTarget(self, action: #selector(handleVerify), for: .primaryActionTriggered)

        viewModel.onStateChange = { [weak self] state in self?.render(state) }
        handleFieldChanged()
    }

    private func configureLayout() {
        let titleLabel = UILabel()
        titleLabel.text = "Check your email"
        titleLabel.museMarkAsHeader()
        titleLabel.font = .preferredFont(forTextStyle: .title2)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0

        let bodyLabel = CredentialScreenViews.makeStatusLabel()
        bodyLabel.text = "We've sent a link to \(email). Open it to finish creating your account.\n\n"
            + "Your account isn't created until you do — nothing is saved yet."

        let stack = UIStackView(arrangedSubviews: [
            titleLabel,
            bodyLabel,
            tokenField,
            verifyButton,
            activityIndicator,
            statusLabel,
            resendButton,
            backButton
        ])
        stack.axis = .vertical
        stack.spacing = 14
        stack.setCustomSpacing(24, after: bodyLabel)
        CredentialScreenViews.embedScrolling(stack, in: view)

        NSLayoutConstraint.activate([
            verifyButton.heightAnchor.constraint(greaterThanOrEqualToConstant: 46)
        ])
    }

    @objc private func handleFieldChanged() {
        verifyButton.isEnabled = !(tokenField.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    @objc private func handleVerify() {
        view.endEditing(true)
        let token = tokenField.text ?? ""
        Task { await viewModel.verify(token: token) }
    }

    @objc private func handleResend() {
        Task { await viewModel.resendVerification(email: email) }
    }

    @objc private func handleBack() {
        onBackToLogIn()
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
            statusLabel.textColor = .systemRed
            statusLabel.text = message
        case .acknowledged(let message):
            setBusy(false)
            showStatus(message, isFailure: false)
        case .authenticated(let result):
            setBusy(true)
            statusLabel.text = nil
            onVerified(result)
        }
    }

    private func setBusy(_ busy: Bool) {
        if busy { activityIndicator.startAnimating() } else { activityIndicator.stopAnimating() }
        verifyButton.isEnabled = !busy && !(tokenField.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        resendButton.isEnabled = !busy
        backButton.isEnabled = !busy
    }

    // MARK: - Test seam

    func testSetToken(_ token: String) {
        tokenField.text = token
        handleFieldChanged()
    }
    func testTapVerify() { handleVerify() }
    func testTapResend() { handleResend() }
    var testVerifyEnabled: Bool { verifyButton.isEnabled }
    var testStatusText: String? { statusLabel.text }
}
