import UIKit

final class PlaceholderDestinationViewController: UIViewController {
    private let message: String
    private let actionTitle: String?
    private let onAction: (() -> Void)?

    init(message: String, actionTitle: String? = nil, onAction: (() -> Void)? = nil) {
        self.message = message
        self.actionTitle = actionTitle
        self.onAction = onAction
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground

        let titleLabel = UILabel()
        titleLabel.text = "Muse"
        titleLabel.font = .museScaled(ofSize: 28, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textColor = .label
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        let messageLabel = UILabel()
        messageLabel.text = message
        messageLabel.font = .museScaled(ofSize: 15, weight: .regular)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .secondaryLabel
        messageLabel.textAlignment = .center
        messageLabel.numberOfLines = 0

        var arrangedSubviews: [UIView] = [titleLabel, messageLabel]

        if let actionTitle {
            var actionConfiguration = UIButton.Configuration.plain()
            actionConfiguration.title = actionTitle
            let actionButton = UIButton(configuration: actionConfiguration)
            actionButton.addTarget(self, action: #selector(handleActionTapped), for: .touchUpInside)
            arrangedSubviews.append(actionButton)
        }

        let stack = UIStackView(arrangedSubviews: arrangedSubviews)
        stack.axis = .vertical
        stack.spacing = 12
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -32)
        ])
    }

    @objc private func handleActionTapped() {
        onAction?()
    }
}
