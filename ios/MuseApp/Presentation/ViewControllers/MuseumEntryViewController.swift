import UIKit

final class MuseumEntryViewController: UIViewController {
    // MARK: - Properties

    private let viewModel: MuseumEntryViewModel
    private let onCreateMuseum: () -> Void
    private let onEnterMuseum: (Museum) -> Void
    private let onChangeStyle: (Museum) -> Void
    private let onOpenPrivacy: (Museum) -> Void
    private let onShareMuseum: (Museum) -> Void
    private let onRegenerateLink: (Museum) -> Void

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let messageLabel = UILabel()
    private let actionStack = UIStackView()

    // MARK: - Initialization

    init(
        viewModel: MuseumEntryViewModel,
        onCreateMuseum: @escaping () -> Void,
        onEnterMuseum: @escaping (Museum) -> Void,
        onChangeStyle: @escaping (Museum) -> Void,
        onOpenPrivacy: @escaping (Museum) -> Void,
        onShareMuseum: @escaping (Museum) -> Void,
        onRegenerateLink: @escaping (Museum) -> Void
    ) {
        self.viewModel = viewModel
        self.onCreateMuseum = onCreateMuseum
        self.onEnterMuseum = onEnterMuseum
        self.onChangeStyle = onChangeStyle
        self.onOpenPrivacy = onOpenPrivacy
        self.onShareMuseum = onShareMuseum
        self.onRegenerateLink = onRegenerateLink
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Museum"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        Task { await viewModel.load() }
    }

    private func configureLayout() {
        messageLabel.font = .museScaled(ofSize: 17, weight: .regular)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .label
        messageLabel.textAlignment = .center
        messageLabel.numberOfLines = 0
        messageLabel.accessibilityTraits.insert(.staticText)

        actionStack.axis = .vertical
        actionStack.spacing = 12
        actionStack.alignment = .center

        let stack = UIStackView(arrangedSubviews: [activityIndicator, messageLabel, actionStack])
        stack.axis = .vertical
        stack.spacing = 20
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

    // MARK: - Rendering

    private func render(_ state: MuseumEntryViewModel.State) {
        actionStack.arrangedSubviews.forEach { $0.removeFromSuperview() }

        switch state {
        case .loading:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Loading your Museum"
            messageLabel.text = nil

        case .needsCreation:
            activityIndicator.stopAnimating()
            messageLabel.text = "You haven't built your Museum yet."
            actionStack.addArrangedSubview(makeButton(
                title: "Create Your Museum",
                prominent: true,
                action: #selector(handleCreateTapped)
            ))

        case .hasMuseum(let museum):
            activityIndicator.stopAnimating()
            messageLabel.text = """
            Your Museum is ready.
            Style: \(museum.styleID)
            Privacy: \(museum.privacy == .public ? "Public" : "Private")
            """
            if let notice = viewModel.refreshFailureNotice {
                messageLabel.text = (messageLabel.text ?? "") + "\n\n" + notice
                actionStack.addArrangedSubview(makeButton(
                    title: "Try Again",
                    prominent: false,
                    action: #selector(handleRetryTapped)
                ))
                MuseAccessibility.announceFailure(notice)
            }
            actionStack.addArrangedSubview(makeButton(
                title: "Enter Museum",
                prominent: true,
                action: #selector(handleEnterTapped)
            ))
            actionStack.addArrangedSubview(makeButton(
                title: "Change Style",
                prominent: false,
                action: #selector(handleChangeStyleTapped)
            ))
            actionStack.addArrangedSubview(makeButton(
                title: "Privacy",
                prominent: false,
                action: #selector(handlePrivacyTapped)
            ))
            actionStack.addArrangedSubview(makeButton(
                title: "Share Museum",
                prominent: false,
                action: #selector(handleShareTapped)
            ))
            actionStack.addArrangedSubview(makeButton(
                title: "New Link",
                prominent: false,
                action: #selector(handleRegenerateLinkTapped)
            ))

        case .failed(let message):
            activityIndicator.stopAnimating()
            messageLabel.text = message
            actionStack.addArrangedSubview(makeButton(
                title: "Try Again",
                prominent: false,
                action: #selector(handleRetryTapped)
            ))
            MuseAccessibility.announceFailure(message)
        }

        activityIndicator.isAccessibilityElement = false
        MuseAccessibility.announceLayoutChange(focusing: messageLabel)
    }

    private func makeButton(title: String, prominent: Bool, action: Selector) -> UIButton {
        var configuration = prominent ? UIButton.Configuration.filled() : UIButton.Configuration.plain()
        configuration.title = title
        configuration.cornerStyle = .medium
        let button = UIButton(configuration: configuration)
        button.addTarget(self, action: action, for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        button.widthAnchor.constraint(greaterThanOrEqualToConstant: 240).isActive = true
        return button
    }

    // MARK: - Actions

    @objc private func handleCreateTapped() {
        guard viewModel.canOfferCreation else { return }
        onCreateMuseum()
    }

    @objc private func handleEnterTapped() {
        guard case .hasMuseum(let museum) = viewModel.state else { return }
        onEnterMuseum(museum)
    }

    @objc private func handleChangeStyleTapped() {
        guard case .hasMuseum(let museum) = viewModel.state else { return }
        onChangeStyle(museum)
    }

    @objc private func handlePrivacyTapped() {
        guard case .hasMuseum(let museum) = viewModel.state else { return }
        onOpenPrivacy(museum)
    }

    @objc private func handleShareTapped() {
        guard case .hasMuseum(let museum) = viewModel.state else { return }
        onShareMuseum(museum)
    }

    @objc private func handleRegenerateLinkTapped() {
        guard case .hasMuseum(let museum) = viewModel.state else { return }
        onRegenerateLink(museum)
    }

    @objc private func handleRetryTapped() {
        Task { await viewModel.load() }
    }
}
