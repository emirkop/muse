import UIKit

final class CollectionDesignSelectionViewController: UIViewController {
    // MARK: - Properties

    private let viewModel: CollectionDesignSelectionViewModel
    private let onPreviewDesign: (CollectionDesign) -> Void
    private let onApplied: (CollectionRoom) -> Void

    private let scrollView = UIScrollView()
    private let contentStack = UIStackView()
    private let headlineLabel = UILabel()
    private let loadingIndicator = UIActivityIndicatorView(style: .medium)
    private let statusLabel = UILabel()
    private let retryButton = UIButton(configuration: .bordered())
    private let designStack = UIStackView()
    private let errorLabel = UILabel()

    private var designButtons: [String: UIButton] = [:]

    // MARK: - Initialization

    init(
        viewModel: CollectionDesignSelectionViewModel,
        onPreviewDesign: @escaping (CollectionDesign) -> Void,
        onApplied: @escaping (CollectionRoom) -> Void
    ) {
        self.viewModel = viewModel
        self.onPreviewDesign = onPreviewDesign
        self.onApplied = onApplied
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        title = viewModel.selectedDesignID == nil ? "Choose a Design" : "Change Design"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        render(viewModel.state)
        Task { await viewModel.load() }
    }

    func applyDesign(id: String) {
        Task {
            if let room = await viewModel.select(designID: id) {
                onApplied(room)
            } else {
                render(viewModel.state)
            }
        }
    }

    private func configureLayout() {
        headlineLabel.text = "Pick how this Collection Room looks."
        headlineLabel.font = .museScaled(ofSize: 15)
        headlineLabel.adjustsFontForContentSizeCategory = true
        headlineLabel.museMarkAsHeader()
        headlineLabel.textColor = .secondaryLabel
        headlineLabel.numberOfLines = 0

        loadingIndicator.hidesWhenStopped = true

        statusLabel.font = .museScaled(ofSize: 15)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .secondaryLabel
        statusLabel.numberOfLines = 0
        statusLabel.textAlignment = .center

        retryButton.setTitle("Try Again", for: .normal)
        retryButton.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.load() }
        }, for: .touchUpInside)

        designStack.axis = .vertical
        designStack.spacing = 10

        errorLabel.font = .museScaled(ofSize: 13)
        errorLabel.adjustsFontForContentSizeCategory = true
        errorLabel.textColor = .systemRed
        errorLabel.numberOfLines = 0

        contentStack.axis = .vertical
        contentStack.spacing = 16
        contentStack.translatesAutoresizingMaskIntoConstraints = false
        [headlineLabel, loadingIndicator, statusLabel, retryButton, designStack, errorLabel]
            .forEach(contentStack.addArrangedSubview)

        scrollView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.addSubview(contentStack)
        view.addSubview(scrollView)

        NSLayoutConstraint.activate([
            scrollView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            scrollView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            scrollView.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor),

            contentStack.topAnchor.constraint(equalTo: scrollView.topAnchor, constant: 24),
            contentStack.leadingAnchor.constraint(equalTo: scrollView.leadingAnchor, constant: 24),
            contentStack.trailingAnchor.constraint(equalTo: scrollView.trailingAnchor, constant: -24),
            contentStack.bottomAnchor.constraint(equalTo: scrollView.bottomAnchor, constant: -24),
            contentStack.widthAnchor.constraint(equalTo: scrollView.widthAnchor, constant: -48)
        ])
    }

    // MARK: - Rendering

    private func render(_ state: CollectionDesignSelectionViewModel.State) {
        errorLabel.text = viewModel.selectionErrorMessage
        errorLabel.isHidden = viewModel.selectionErrorMessage == nil

        switch state {
        case .loading:
            loadingIndicator.startAnimating()
            statusLabel.isHidden = true
            retryButton.isHidden = true

        case .loaded(let designs, let selected):
            loadingIndicator.stopAnimating()
            statusLabel.isHidden = true
            retryButton.isHidden = true
            renderDesignCards(designs, selected: selected)

        case .noDesignsAvailable:
            loadingIndicator.stopAnimating()
            statusLabel.text = CollectionDesignSelectionViewModel.noDesignsMessage
            statusLabel.isHidden = false
            retryButton.isHidden = true
            renderDesignCards([], selected: nil)

        case .failed(let message):
            loadingIndicator.stopAnimating()
            statusLabel.text = message
            statusLabel.isHidden = false
            retryButton.isHidden = false
            renderDesignCards([], selected: nil)
        }
    }

    private func renderDesignCards(_ designs: [CollectionDesign], selected: String?) {
        if Set(designButtons.keys) != Set(designs.map(\.id)) {
            designStack.arrangedSubviews.forEach { $0.removeFromSuperview() }
            designButtons = [:]
            for design in designs {
                let button = makeDesignCard(for: design)
                designButtons[design.id] = button
                designStack.addArrangedSubview(button)
            }
        }
        for (id, button) in designButtons {
            let isSelected = id == selected
            button.configuration?.background.strokeColor = isSelected ? .tintColor : .separator
            button.configuration?.background.strokeWidth = isSelected ? 2 : 1
            button.accessibilityTraits = isSelected ? [.button, .selected] : [.button]
            button.isEnabled = !viewModel.isSaving
        }
    }

    private func makeDesignCard(for design: CollectionDesign) -> UIButton {
        var configuration = UIButton.Configuration.plain()
        configuration.title = design.displayName
        configuration.subtitle = viewModel.subtitle(for: design)
        configuration.image = UIImage(systemName: design.isDevelopmentFixture ? "hammer" : "cube.transparent")
        configuration.imagePadding = 12
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 16, leading: 16, bottom: 16, trailing: 16)
        configuration.background.cornerRadius = 12
        configuration.background.strokeColor = .separator
        configuration.background.strokeWidth = 1

        let button = UIButton(configuration: configuration)
        button.contentHorizontalAlignment = .leading
        button.accessibilityLabel = design.displayName
        button.addAction(UIAction { [weak self] _ in
            self?.onPreviewDesign(design)
        }, for: .touchUpInside)
        return button
    }
}
