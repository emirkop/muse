import UIKit

final class RoomVariantSelectionViewController: UIViewController {
    private let viewModel: RoomVariantSelectionViewModel
    private let onPreviewVariant: (RoomVariant) -> Void
    private let onApplied: (Room) -> Void

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let headlineLabel = UILabel()
    private let variantStack = UIStackView()
    private let statusLabel = UILabel()
    private let retryButton = UIButton(configuration: .bordered())

    init(
        viewModel: RoomVariantSelectionViewModel,
        onPreviewVariant: @escaping (RoomVariant) -> Void,
        onApplied: @escaping (Room) -> Void
    ) {
        self.viewModel = viewModel
        self.onPreviewVariant = onPreviewVariant
        self.onApplied = onApplied
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = viewModel.currentVariantID == nil ? "Choose a Design" : "Change Design"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        Task { await viewModel.load() }
    }

    private func configureLayout() {
        headlineLabel.text = "Pick this Room's design. These belong to your Museum's current style."
        headlineLabel.font = .museScaled(ofSize: 15, weight: .regular)
        headlineLabel.adjustsFontForContentSizeCategory = true
        headlineLabel.textColor = .secondaryLabel
        headlineLabel.textAlignment = .center
        headlineLabel.numberOfLines = 0
        headlineLabel.museMarkAsHeader()

        variantStack.axis = .vertical
        variantStack.spacing = 10
        variantStack.alignment = .center

        statusLabel.font = .museScaled(ofSize: 14, weight: .regular)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .secondaryLabel
        statusLabel.textAlignment = .center
        statusLabel.numberOfLines = 0

        retryButton.configuration?.title = "Try Again"
        retryButton.titleLabel?.adjustsFontForContentSizeCategory = true
        retryButton.accessibilityIdentifier = "variant-selection-retry"
        retryButton.isHidden = true
        retryButton.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.load() }
        }, for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [headlineLabel, activityIndicator, variantStack, statusLabel, retryButton])
        stack.axis = .vertical
        stack.spacing = 20
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -24)
        ])
    }

    private func render(_ state: RoomVariantSelectionViewModel.State) {
        retryButton.isHidden = true
        switch state {
        case .loading, .applying:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Loading designs"
            statusLabel.text = nil

        case .ready(let variants):
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            statusLabel.text = nil
            renderVariantCards(variants)

        case .applied(let room):
            activityIndicator.stopAnimating()
            onApplied(room)

        case .failed(let message, let variants):
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            statusLabel.text = message
            renderVariantCards(variants)
            retryButton.isHidden = !variants.isEmpty
            MuseAccessibility.announceFailure(message)
        }
    }

    private func renderVariantCards(_ variants: [RoomVariant]) {
        variantStack.arrangedSubviews.forEach { $0.removeFromSuperview() }

        for variant in variants {
            var configuration = UIButton.Configuration.bordered()
            configuration.title = variant.displayName
            configuration.subtitle = viewModel.isCurrentlySelected(variant) ? "Currently Selected" : "Tap to preview"
            configuration.cornerStyle = .medium
            let card = UIButton(configuration: configuration)
            card.addAction(UIAction { [weak self] _ in self?.onPreviewVariant(variant) }, for: .touchUpInside)
            card.translatesAutoresizingMaskIntoConstraints = false
            card.widthAnchor.constraint(greaterThanOrEqualToConstant: 260).isActive = true
            if viewModel.isCurrentlySelected(variant) {
                card.accessibilityTraits = [.button, .selected]
            }
            variantStack.addArrangedSubview(card)
        }
        MuseAccessibility.announceLayoutChange(focusing: variantStack.arrangedSubviews.first)
    }

    func applyVariant(_ variantID: String) {
        Task { await viewModel.chooseVariant(variantID) }
    }
}
