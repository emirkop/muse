import UIKit

final class LaunchLoadingViewController: UIViewController {
    var onRetry: (() -> Void)?

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let messageLabel = UILabel()
    private let retryButton = UIButton(configuration: .bordered())

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        configureUnreachableViews()
        activityIndicator.translatesAutoresizingMaskIntoConstraints = false
        activityIndicator.startAnimating()
        activityIndicator.isAccessibilityElement = false
        view.addSubview(activityIndicator)
        announceLoading()

        NSLayoutConstraint.activate([
            activityIndicator.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            activityIndicator.centerYAnchor.constraint(equalTo: view.centerYAnchor)
        ])
    }

    private func configureUnreachableViews() {
        messageLabel.font = .preferredFont(forTextStyle: .body)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .secondaryLabel
        messageLabel.numberOfLines = 0
        messageLabel.textAlignment = .center
        messageLabel.isHidden = true
        messageLabel.accessibilityIdentifier = "launch-unreachable-message"

        retryButton.configuration?.title = "Try Again"
        retryButton.titleLabel?.adjustsFontForContentSizeCategory = true
        retryButton.isHidden = true
        retryButton.accessibilityIdentifier = "launch-retry"
        retryButton.addAction(UIAction { [weak self] _ in
            self?.showRetrying()
            self?.onRetry?()
        }, for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [messageLabel, retryButton])
        stack.axis = .vertical
        stack.spacing = 16
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.topAnchor.constraint(equalTo: view.centerYAnchor, constant: 32),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -24)
        ])
    }

    func showServerUnreachable(message: String) {
        loadViewIfNeeded()
        activityIndicator.stopAnimating()
        view.isAccessibilityElement = false
        view.accessibilityLabel = nil
        view.accessibilityTraits.remove(.updatesFrequently)
        messageLabel.text = message
        messageLabel.isHidden = false
        retryButton.isHidden = false
        MuseAccessibility.announceFailure(message)
    }

    private func showRetrying() {
        messageLabel.isHidden = true
        retryButton.isHidden = true
        announceLoading()
        activityIndicator.startAnimating()
    }

    private func announceLoading() {
        view.isAccessibilityElement = true
        view.accessibilityLabel = "Opening Muse"
        view.accessibilityTraits.insert(.updatesFrequently)
    }
}
