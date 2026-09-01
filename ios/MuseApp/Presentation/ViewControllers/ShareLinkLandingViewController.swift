import UIKit

final class ShareLinkLandingViewController: UIViewController {
    private let viewModel: ShareLinkLandingViewModel
    private let onSignIn: () -> Void
    private let onEntered: (SharedMuseumContent) -> Void
    private let onDismiss: () -> Void

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let avatarView = UIImageView(image: UIImage(systemName: "person.crop.circle.fill"))
    private let headingLabel = UILabel()
    private let detailLabel = UILabel()
    private let noticeLabel = UILabel()
    private var lastAnnouncedStateIdentity: String?
    private let primaryButton = UIButton(configuration: .filled())
    private let secondaryButton = UIButton(configuration: .plain())

    init(
        viewModel: ShareLinkLandingViewModel,
        onSignIn: @escaping () -> Void,
        onEntered: @escaping (SharedMuseumContent) -> Void,
        onDismiss: @escaping () -> Void
    ) {
        self.viewModel = viewModel
        self.onSignIn = onSignIn
        self.onEntered = onEntered
        self.onDismiss = onDismiss
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Invitation"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        Task { await viewModel.load() }
    }

    private func configureLayout() {
        avatarView.contentMode = .scaleAspectFit
        avatarView.tintColor = .secondaryLabel
        avatarView.translatesAutoresizingMaskIntoConstraints = false
        avatarView.isAccessibilityElement = false

        headingLabel.font = .museScaled(ofSize: 22, weight: .semibold)
        headingLabel.adjustsFontForContentSizeCategory = true
        headingLabel.textAlignment = .center
        headingLabel.numberOfLines = 0
        headingLabel.museMarkAsHeader()

        detailLabel.font = .museScaled(ofSize: 15)
        detailLabel.adjustsFontForContentSizeCategory = true
        detailLabel.textColor = .secondaryLabel
        detailLabel.textAlignment = .center
        detailLabel.numberOfLines = 0

        noticeLabel.font = .museScaled(ofSize: 14, weight: .medium)
        noticeLabel.adjustsFontForContentSizeCategory = true
        noticeLabel.textColor = .systemRed
        noticeLabel.textAlignment = .center
        noticeLabel.numberOfLines = 0
        noticeLabel.isHidden = true

        var primary = UIButton.Configuration.filled()
        primary.cornerStyle = .medium
        primaryButton.configuration = primary
        primaryButton.addTarget(self, action: #selector(handlePrimary), for: .touchUpInside)
        primaryButton.translatesAutoresizingMaskIntoConstraints = false

        var secondary = UIButton.Configuration.plain()
        secondary.title = "Not now"
        secondaryButton.configuration = secondary
        secondaryButton.addTarget(self, action: #selector(handleDismiss), for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [activityIndicator, avatarView, headingLabel, detailLabel, noticeLabel, primaryButton, secondaryButton])
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
            avatarView.widthAnchor.constraint(equalToConstant: 72),
            avatarView.heightAnchor.constraint(equalToConstant: 72),
            primaryButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 240)
        ])
    }

    private func announceStateIfChanged() {
        let identity = String(String(describing: viewModel.state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity
        activityIndicator.isAccessibilityElement = activityIndicator.isAnimating

        let message = [headingLabel.text, detailLabel.text].compactMap { $0 }.joined(separator: ". ")
        switch viewModel.state {
        case .unavailable, .failed:
            MuseAccessibility.announceFailure(message)
        default:
            MuseAccessibility.announce(message)
        }
    }

    private func render(_ state: ShareLinkLandingViewModel.State) {
        noticeLabel.isHidden = true
        switch state {
        case .loading:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Opening this link"
            avatarView.isHidden = true
            headingLabel.text = nil
            detailLabel.text = nil
            primaryButton.isHidden = true
            secondaryButton.isHidden = true

        case .available(let preview):
            activityIndicator.stopAnimating()
            avatarView.isHidden = false
            headingLabel.text = ShareLinkLandingViewModel.heading
            let avatarName = AvatarCatalog.all.first(where: { $0.id == preview.ownerAvatarID })?.displayName
            detailLabel.text = [avatarName.map { "Avatar: \($0)" }, "Style: \(preview.styleID)"]
                .compactMap { $0 }
                .joined(separator: "\n")
            primaryButton.isHidden = false
            primaryButton.configuration?.title = viewModel.primaryActionTitle
            primaryButton.isEnabled = true
            secondaryButton.isHidden = false

        case .unavailable:
            activityIndicator.stopAnimating()
            avatarView.isHidden = true
            headingLabel.text = "This link is no longer available."
            detailLabel.text = "The Museum it pointed to can't be reached with this link."
            primaryButton.isHidden = false
            primaryButton.configuration?.title = "OK"
            primaryButton.isEnabled = true
            secondaryButton.isHidden = true

        case .failed(let message):
            activityIndicator.stopAnimating()
            avatarView.isHidden = true
            headingLabel.text = message
            detailLabel.text = nil
            primaryButton.isHidden = false
            primaryButton.configuration?.title = "Try Again"
            primaryButton.isEnabled = true
            secondaryButton.isHidden = false
        }

        announceStateIfChanged()
    }

    @objc private func handlePrimary() {
        switch viewModel.state {
        case .available:
            primaryButton.isEnabled = false
            Task { await enter() }
        case .unavailable:
            onDismiss()
        case .failed:
            Task { await viewModel.load() }
        case .loading:
            break
        }
    }

    private func enter() async {
        switch await viewModel.enter() {
        case .needsAuthentication:
            onSignIn()
        case .entered(let content):
            onEntered(content)
        case .unavailable:
            break
        case .failed(let message):
            primaryButton.isEnabled = true
            noticeLabel.text = message
            noticeLabel.isHidden = false
            MuseAccessibility.announceFailure(message)
        }
    }

    @objc private func handleDismiss() {
        onDismiss()
    }
}

// MARK: - Test seams

extension ShareLinkLandingViewController {
    var testHeadingText: String? { headingLabel.text }
    var testDetailText: String? { detailLabel.text }
    var testPrimaryTitle: String? { primaryButton.configuration?.title }
    var testPrimaryIsHidden: Bool { primaryButton.isHidden }
    func testTapPrimary() { handlePrimary() }
}
