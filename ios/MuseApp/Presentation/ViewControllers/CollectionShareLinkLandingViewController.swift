import UIKit

final class CollectionShareLinkLandingViewController: UIViewController {
    private let viewModel: CollectionShareLinkLandingViewModel
    private let onSignIn: () -> Void
    private let onEntered: (SharedCollectionRoomContent) -> Void
    private let onDismiss: () -> Void

    private let iconView = UIImageView(image: UIImage(systemName: "square.grid.2x2"))
    private let headingLabel = UILabel()
    private let detailLabel = UILabel()
    private let noticeLabel = UILabel()
    private var lastAnnouncedStateIdentity: String?
    private let primaryButton = UIButton(configuration: .filled())
    private let secondaryButton = UIButton(configuration: .plain())

    init(
        viewModel: CollectionShareLinkLandingViewModel,
        onSignIn: @escaping () -> Void,
        onEntered: @escaping (SharedCollectionRoomContent) -> Void,
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
        render(viewModel.state)
    }

    private func configureLayout() {
        iconView.contentMode = .scaleAspectFit
        iconView.tintColor = .secondaryLabel
        iconView.translatesAutoresizingMaskIntoConstraints = false

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

        let stack = UIStackView(arrangedSubviews: [iconView, headingLabel, detailLabel, noticeLabel, primaryButton, secondaryButton])
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
            iconView.widthAnchor.constraint(equalToConstant: 72),
            iconView.heightAnchor.constraint(equalToConstant: 72),
            primaryButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 240)
        ])

        iconView.isAccessibilityElement = false
    }

    private func announceStateIfChanged() {
        let identity = String(String(describing: viewModel.state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity
        let message = [headingLabel.text, detailLabel.text].compactMap { $0 }.joined(separator: ". ")
        switch viewModel.state {
        case .unavailable, .failed:
            MuseAccessibility.announceFailure(message)
        default:
            MuseAccessibility.announce(message)
        }
    }

    private func render(_ state: CollectionShareLinkLandingViewModel.State) {
        noticeLabel.isHidden = true
        switch state {
        case .ready:
            iconView.isHidden = false
            headingLabel.text = CollectionShareLinkLandingViewModel.heading
            detailLabel.text = CollectionShareLinkLandingViewModel.detail
            primaryButton.isHidden = false
            primaryButton.configuration?.title = viewModel.primaryActionTitle
            primaryButton.isEnabled = true
            secondaryButton.isHidden = false

        case .unavailable:
            iconView.isHidden = true
            headingLabel.text = "This link is no longer available."
            detailLabel.text = "The Collection Room it pointed to can't be reached with this link."
            primaryButton.isHidden = false
            primaryButton.configuration?.title = "OK"
            primaryButton.isEnabled = true
            secondaryButton.isHidden = true

        case .failed(let message):
            iconView.isHidden = true
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
        case .ready:
            primaryButton.isEnabled = false
            Task { await enter() }
        case .unavailable:
            onDismiss()
        case .failed:
            viewModel.reset()
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

extension CollectionShareLinkLandingViewController {
    var testHeadingText: String? { headingLabel.text }
    var testDetailText: String? { detailLabel.text }
    var testPrimaryTitle: String? { primaryButton.configuration?.title }
    func testTapPrimary() { handlePrimary() }
}
