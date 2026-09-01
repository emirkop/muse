import AuthenticationServices
import UIKit

final class AuthenticationViewController: UIViewController {
    private let viewModel: AuthenticationViewModel
    private let onSignedIn: (LoginResult) -> Void

    private let appleSignInButton = ASAuthorizationAppleIDButton(type: .signIn, style: .black)
    private let googleSignInButton = AuthenticationViewController.makeGoogleSignInButton()
    private let statusLabel = UILabel()
    private let activityIndicator = UIActivityIndicatorView(style: .medium)

    init(viewModel: AuthenticationViewModel, onSignedIn: @escaping (LoginResult) -> Void) {
        self.viewModel = viewModel
        self.onSignedIn = onSignedIn
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        configureLayout()

        appleSignInButton.addTarget(self, action: #selector(handleAppleSignInTapped), for: .touchUpInside)
        googleSignInButton.addTarget(self, action: #selector(handleGoogleSignInTapped), for: .touchUpInside)
        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
    }

    private func configureLayout() {
        let titleLabel = UILabel()
        titleLabel.text = "Muse"
        titleLabel.font = .museScaled(ofSize: 28, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textColor = .label
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        statusLabel.font = .museScaled(ofSize: 14, weight: .regular)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .secondaryLabel
        statusLabel.textAlignment = .center
        statusLabel.numberOfLines = 0

        appleSignInButton.translatesAutoresizingMaskIntoConstraints = false
        googleSignInButton.translatesAutoresizingMaskIntoConstraints = false

        let stack = UIStackView(arrangedSubviews: [titleLabel, appleSignInButton, googleSignInButton, activityIndicator, statusLabel])
        stack.axis = .vertical
        stack.spacing = 16
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -32),
            appleSignInButton.widthAnchor.constraint(equalToConstant: 260),
            appleSignInButton.heightAnchor.constraint(greaterThanOrEqualToConstant: 44),
            googleSignInButton.widthAnchor.constraint(equalTo: appleSignInButton.widthAnchor),
            googleSignInButton.heightAnchor.constraint(greaterThanOrEqualTo: appleSignInButton.heightAnchor)
        ])
    }

    private static func makeGoogleSignInButton() -> UIButton {
        var configuration = UIButton.Configuration.plain()
        configuration.title = "Sign in with Google"
        configuration.baseForegroundColor = .label
        configuration.background.backgroundColor = .systemBackground
        configuration.background.strokeColor = .separator
        configuration.background.strokeWidth = 1
        configuration.cornerStyle = .medium
        return UIButton(configuration: configuration)
    }

    @objc private func handleAppleSignInTapped() {
        Task { await viewModel.signInWithApple() }
    }

    @objc private func handleGoogleSignInTapped() {
        Task { await viewModel.signInWithGoogle() }
    }

    private func render(_ state: AuthenticationViewModel.State) {
        switch state {
        case .idle:
            activityIndicator.stopAnimating()
            appleSignInButton.isEnabled = true
            googleSignInButton.isEnabled = true
            statusLabel.text = nil
        case .loading:
            activityIndicator.startAnimating()
            appleSignInButton.isEnabled = false
            googleSignInButton.isEnabled = false
            statusLabel.text = nil
            MuseAccessibility.announce("Signing in")
        case .failed(let message):
            activityIndicator.stopAnimating()
            appleSignInButton.isEnabled = true
            googleSignInButton.isEnabled = true
            statusLabel.text = message
            MuseAccessibility.announceFailure(message)
        case .succeeded(let result):
            activityIndicator.stopAnimating()
            appleSignInButton.isEnabled = false
            googleSignInButton.isEnabled = false
            statusLabel.text = nil
            onSignedIn(result)
        }
    }
}
