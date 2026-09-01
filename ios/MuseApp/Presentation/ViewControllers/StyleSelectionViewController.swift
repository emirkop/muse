import UIKit

final class StyleSelectionViewController: UIViewController {
    private let viewModel: StyleSelectionViewModel
    private let onPreviewStyle: (MuseumStyle) -> Void
    private let onApplied: (Museum) -> Void

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let headlineLabel = UILabel()
    private let styleStack = UIStackView()
    private let gateNoticeLabel = UILabel()
    private let statusLabel = UILabel()
    private let retryButton = UIButton(configuration: .bordered())

    init(
        viewModel: StyleSelectionViewModel,
        onPreviewStyle: @escaping (MuseumStyle) -> Void,
        onApplied: @escaping (Museum) -> Void
    ) {
        self.viewModel = viewModel
        self.onPreviewStyle = onPreviewStyle
        self.onApplied = onApplied
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = viewModel.requiresContentPreservationReassurance ? "Change Style" : "Choose a Style"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        Task { await viewModel.load() }
    }

    private func configureLayout() {
        headlineLabel.text = "Each style is a different environment — architecture, materials and lighting, not just colour."
        headlineLabel.font = .museScaled(ofSize: 15, weight: .regular)
        headlineLabel.adjustsFontForContentSizeCategory = true
        headlineLabel.textColor = .secondaryLabel
        headlineLabel.textAlignment = .center
        headlineLabel.numberOfLines = 0
        headlineLabel.museMarkAsHeader()

        styleStack.axis = .vertical
        styleStack.spacing = 10
        styleStack.alignment = .center

        gateNoticeLabel.text = StyleSelectionViewModel.openStyleGateNotice
        gateNoticeLabel.font = .museScaled(ofSize: 13, weight: .regular)
        gateNoticeLabel.adjustsFontForContentSizeCategory = true
        gateNoticeLabel.textColor = .tertiaryLabel
        gateNoticeLabel.textAlignment = .center
        gateNoticeLabel.numberOfLines = 0

        statusLabel.font = .museScaled(ofSize: 14, weight: .regular)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .secondaryLabel
        statusLabel.textAlignment = .center
        statusLabel.numberOfLines = 0

        retryButton.configuration?.title = "Try Again"
        retryButton.titleLabel?.adjustsFontForContentSizeCategory = true
        retryButton.accessibilityIdentifier = "style-selection-retry"
        retryButton.isHidden = true
        retryButton.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.load() }
        }, for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [
            headlineLabel, activityIndicator, styleStack, gateNoticeLabel, statusLabel, retryButton
        ])
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

    private func render(_ state: StyleSelectionViewModel.State) {
        retryButton.isHidden = true
        switch state {
        case .loading, .applying:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Loading styles"
            statusLabel.text = nil

        case .ready(let styles):
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            statusLabel.text = nil
            renderStyleCards(styles)

        case .applied(let museum):
            activityIndicator.stopAnimating()
            onApplied(museum)

        case .failed(let message, let styles):
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            statusLabel.text = message
            renderStyleCards(styles)
            retryButton.isHidden = !styles.isEmpty
            MuseAccessibility.announceFailure(message)
        }
    }

    private func renderStyleCards(_ styles: [MuseumStyle]) {
        styleStack.arrangedSubviews.forEach { $0.removeFromSuperview() }

        for style in styles {
            var configuration = UIButton.Configuration.bordered()
            configuration.title = style.displayName
            configuration.subtitle = viewModel.isCurrentlySelected(style) ? "Currently Selected" : "Tap to preview"
            configuration.cornerStyle = .medium
            let card = UIButton(configuration: configuration)
            card.addAction(UIAction { [weak self] _ in self?.onPreviewStyle(style) }, for: .touchUpInside)
            card.translatesAutoresizingMaskIntoConstraints = false
            card.widthAnchor.constraint(greaterThanOrEqualToConstant: 260).isActive = true
            if viewModel.isCurrentlySelected(style) {
                card.accessibilityTraits = [.button, .selected]
            }
            styleStack.addArrangedSubview(card)
        }
        MuseAccessibility.announceLayoutChange(focusing: styleStack.arrangedSubviews.first)
    }

    func applyStyle(_ styleID: String) {
        Task { await viewModel.chooseStyle(styleID) }
    }
}
