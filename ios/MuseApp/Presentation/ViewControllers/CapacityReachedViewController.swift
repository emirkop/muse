import UIKit

final class CapacityReachedViewController: UIViewController {
    private let viewModel: CapacityViewModel
    private let onUpgraded: (AccountEntitlement) -> Void
    private let onDismiss: () -> Void

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let headingLabel = UILabel()
    private let detailLabel = UILabel()
    private let noticeLabel = UILabel()
    private var lastAnnouncedStateIdentity: String?
    private let upgradeButton = UIButton(configuration: .filled())
    private let restoreButton = UIButton(configuration: .plain())
    private let dismissButton = UIButton(configuration: .plain())

    init(viewModel: CapacityViewModel, onUpgraded: @escaping (AccountEntitlement) -> Void, onDismiss: @escaping () -> Void) {
        self.viewModel = viewModel
        self.onUpgraded = onUpgraded
        self.onDismiss = onDismiss
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Collection Capacity"
        view.backgroundColor = .systemBackground
        configureLayout()
        viewModel.onStateChange = { [weak self] state in self?.render(state) }
        render(viewModel.state)
        Task { await viewModel.load() }
    }

    private func configureLayout() {
        headingLabel.font = .museScaled(ofSize: 22, weight: .semibold)
        headingLabel.adjustsFontForContentSizeCategory = true
        headingLabel.museMarkAsHeader()
        headingLabel.textAlignment = .center
        headingLabel.numberOfLines = 0
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

        var upgrade = UIButton.Configuration.filled()
        upgrade.cornerStyle = .medium
        upgradeButton.configuration = upgrade
        upgradeButton.accessibilityIdentifier = "capacity-upgrade"
        upgradeButton.addAction(UIAction { [weak self] _ in Task { await self?.viewModel.purchase() } }, for: .touchUpInside)
        upgradeButton.translatesAutoresizingMaskIntoConstraints = false

        restoreButton.configuration?.title = "Restore Purchases"
        restoreButton.accessibilityIdentifier = "capacity-restore"
        restoreButton.addAction(UIAction { [weak self] _ in Task { await self?.viewModel.restore() } }, for: .touchUpInside)

        dismissButton.configuration?.title = "Not now"
        dismissButton.addAction(UIAction { [weak self] _ in self?.onDismiss() }, for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [activityIndicator, headingLabel, detailLabel, noticeLabel, upgradeButton, restoreButton, dismissButton])
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
            upgradeButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 260),
        ])
    }

    private func render(_ state: CapacityViewModel.State) {
        noticeLabel.isHidden = true
        switch state {
        case .loading, .purchasing, .restoring:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = state == .loading
                ? "Checking your capacity"
                : (state == .purchasing ? "Completing your purchase" : "Restoring purchases")
            headingLabel.text = state == .loading ? nil : (state == .purchasing ? "Completing your purchase…" : "Restoring purchases…")
            detailLabel.text = nil
            upgradeButton.isHidden = true
            restoreButton.isHidden = true
            dismissButton.isHidden = true

        case .ready(let entitlement, let product):
            activityIndicator.stopAnimating()
            headingLabel.text = entitlement.isAtCapacity ? "Your collections are at capacity." : "Collection capacity"
            detailLabel.text = CapacityViewModel.capacityMessage(entitlement)
            if let product, entitlement.canUpgrade {
                upgradeButton.isHidden = false
                upgradeButton.configuration?.title = "\(product.displayName) — \(product.displayPrice)"
                upgradeButton.accessibilityLabel = "\(product.displayName), \(product.displayPrice)"
                upgradeButton.accessibilityHint = "Unlocks the higher Collection Item capacity"
            } else {
                upgradeButton.isHidden = true
            }
            restoreButton.isHidden = false
            dismissButton.isHidden = false

        case .upgraded(let entitlement):
            activityIndicator.stopAnimating()
            headingLabel.text = "Upgrade confirmed."
            detailLabel.text = CapacityViewModel.capacityMessage(entitlement)
            upgradeButton.isHidden = true
            restoreButton.isHidden = true
            dismissButton.isHidden = false
            dismissButton.configuration?.title = "Continue"
            onUpgraded(entitlement)

        case .failed(let message, let entitlement):
            activityIndicator.stopAnimating()
            headingLabel.text = entitlement.map { $0.isAtCapacity ? "Your collections are at capacity." : "Collection capacity" } ?? "Collection capacity"
            detailLabel.text = entitlement.map(CapacityViewModel.capacityMessage)
            noticeLabel.text = message
            noticeLabel.isHidden = false
            if let product = viewModel.retryableProduct,
               entitlement.map(\.canUpgrade) ?? true {
                upgradeButton.isHidden = false
                upgradeButton.configuration?.title = "\(product.displayName) — \(product.displayPrice)"
                upgradeButton.accessibilityLabel = "\(product.displayName), \(product.displayPrice)"
            } else {
                upgradeButton.isHidden = true
            }
            restoreButton.isHidden = false
            dismissButton.isHidden = false
        }

        announceStateIfChanged(state)
    }

    private func announceStateIfChanged(_ state: CapacityViewModel.State) {
        let identity = String(String(describing: state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity
        activityIndicator.isAccessibilityElement = activityIndicator.isAnimating

        switch state {
        case .failed(let message, _):
            MuseAccessibility.announceFailure(message)
        default:
            let message = [headingLabel.text, detailLabel.text].compactMap { $0 }.joined(separator: ". ")
            MuseAccessibility.announce(message)
        }
    }
}

// MARK: - Test seams

extension CapacityReachedViewController {
    var testHeadingText: String? { headingLabel.text }
    var testDetailText: String? { detailLabel.text }
    var testNoticeText: String? { noticeLabel.isHidden ? nil : noticeLabel.text }
    var testUpgradeTitle: String? { upgradeButton.isHidden ? nil : upgradeButton.configuration?.title }
}
