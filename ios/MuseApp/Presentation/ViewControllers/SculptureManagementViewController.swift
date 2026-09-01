import UIKit

public final class SculptureManagementViewController: UIViewController {
    // MARK: - Properties

    private let viewModel: SculptureManagementViewModel
    private let coordinator: RoomSculptureEditCoordinator
    private let onFinished: () -> Void

    private let scrollView = UIScrollView()
    private let stack = UIStackView()
    private let titleLabel = UILabel()
    private let capacityLabel = UILabel()
    private let noticeLabel = UILabel()
    private let placedHeader = UILabel()
    private let placedStack = UIStackView()
    private let catalogHeader = UILabel()
    private let catalogStack = UIStackView()
    private let spinner = UIActivityIndicatorView(style: .medium)
    private let doneButton = UIButton(configuration: .filled())

    // MARK: - Initialization

    public init(
        viewModel: SculptureManagementViewModel,
        coordinator: RoomSculptureEditCoordinator,
        onFinished: @escaping () -> Void
    ) {
        self.viewModel = viewModel
        self.coordinator = coordinator
        self.onFinished = onFinished
        super.init(nibName: nil, bundle: nil)
        modalPresentationStyle = .pageSheet
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    public override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        buildLayout()
        viewModel.onChange = { [weak self] in self?.render() }
        render()
        Task { await viewModel.load() }
    }

    // MARK: - Layout

    private func buildLayout() {
        titleLabel.text = "Sculptures"
        titleLabel.font = .preferredFont(forTextStyle: .largeTitle)
        titleLabel.adjustsFontForContentSizeCategory = true

        capacityLabel.font = .preferredFont(forTextStyle: .subheadline)
        capacityLabel.adjustsFontForContentSizeCategory = true
        capacityLabel.textColor = .secondaryLabel
        capacityLabel.numberOfLines = 0
        capacityLabel.accessibilityIdentifier = "sculpture-capacity"

        noticeLabel.font = .preferredFont(forTextStyle: .footnote)
        noticeLabel.adjustsFontForContentSizeCategory = true
        noticeLabel.textColor = .systemRed
        noticeLabel.numberOfLines = 0
        noticeLabel.isHidden = true
        noticeLabel.accessibilityIdentifier = "sculpture-notice"

        for (label, text) in [(placedHeader, "In this Room"), (catalogHeader, "Add a sculpture")] {
            label.text = text
            label.font = .preferredFont(forTextStyle: .headline)
            label.adjustsFontForContentSizeCategory = true
        }

        for group in [placedStack, catalogStack] {
            group.axis = .vertical
            group.spacing = 8
        }

        doneButton.configuration?.title = "Done"
        doneButton.addTarget(self, action: #selector(handleDone), for: .touchUpInside)
        doneButton.accessibilityIdentifier = "sculpture-done"

        stack.axis = .vertical
        stack.spacing = 16
        stack.translatesAutoresizingMaskIntoConstraints = false
        for subview in [titleLabel, capacityLabel, noticeLabel, placedHeader, placedStack, catalogHeader, catalogStack, spinner, doneButton] {
            stack.addArrangedSubview(subview)
        }

        scrollView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.addSubview(stack)
        view.addSubview(scrollView)

        NSLayoutConstraint.activate([
            scrollView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            scrollView.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            scrollView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            stack.topAnchor.constraint(equalTo: scrollView.topAnchor, constant: 20),
            stack.bottomAnchor.constraint(equalTo: scrollView.bottomAnchor, constant: -24),
            stack.leadingAnchor.constraint(equalTo: scrollView.leadingAnchor, constant: 20),
            stack.trailingAnchor.constraint(equalTo: scrollView.trailingAnchor, constant: -20),
            stack.widthAnchor.constraint(equalTo: scrollView.widthAnchor, constant: -40)
        ])
    }

    // MARK: - Rendering

    private func render() {
        capacityLabel.text = coordinator.capacityMessage
        noticeLabel.isHidden = viewModel.notice == nil
        noticeLabel.text = viewModel.notice
        if viewModel.state == .loading {
            spinner.startAnimating()
        } else {
            spinner.stopAnimating()
        }

        renderPlaced()
        renderCatalog()
    }

    private func renderPlaced() {
        placedStack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        let placed = coordinator.sculptures
        guard !placed.isEmpty else {
            placedStack.addArrangedSubview(makeMessage("No sculptures in this Room yet."))
            return
        }
        for sculpture in placed {
            placedStack.addArrangedSubview(makePlacedRow(sculpture))
        }
    }

    private func renderCatalog() {
        catalogStack.arrangedSubviews.forEach { $0.removeFromSuperview() }

        switch viewModel.state {
        case .loading:
            catalogStack.addArrangedSubview(makeMessage("Loading the sculpture catalog…"))
            return
        case .failed(let message):
            catalogStack.addArrangedSubview(makeMessage(message))
            catalogStack.addArrangedSubview(makeCatalogRetryButton())
            return
        case .ready:
            break
        }

        guard !viewModel.entries.isEmpty else {
            catalogStack.addArrangedSubview(makeMessage(Self.emptyCatalogMessage))
            return
        }
        if !coordinator.hasCapacity {
            catalogStack.addArrangedSubview(makeMessage(RoomSculptureEditCoordinator.fullMessage))
        }
        for entry in viewModel.entries {
            catalogStack.addArrangedSubview(makeCatalogRow(entry))
        }
    }

    private func makeCatalogRetryButton() -> UIButton {
        var configuration = UIButton.Configuration.bordered()
        configuration.title = "Try Again"
        let button = UIButton(configuration: configuration)
        button.titleLabel?.adjustsFontForContentSizeCategory = true
        button.accessibilityIdentifier = "sculpture-catalog-retry"
        button.addAction(UIAction { [weak self] _ in
            guard let self else { return }
            self.render()
            Task { await self.viewModel.reload() }
        }, for: .touchUpInside)
        return button
    }

    private func makeMessage(_ text: String) -> UILabel {
        let label = UILabel()
        label.text = text
        label.font = .preferredFont(forTextStyle: .body)
        label.adjustsFontForContentSizeCategory = true
        label.textColor = .secondaryLabel
        label.numberOfLines = 0
        return label
    }

    private func makePlacedRow(_ sculpture: SculptureInstance) -> UIView {
        let name = UILabel()
        name.text = viewModel.displayName(forCatalogID: sculpture.catalogID)
        name.font = .preferredFont(forTextStyle: .body)
        name.adjustsFontForContentSizeCategory = true
        name.numberOfLines = 0

        let remove = UIButton(configuration: .plain())
        remove.configuration?.title = "Remove"
        remove.configuration?.baseForegroundColor = .systemRed
        remove.isEnabled = !coordinator.isEditing
        remove.accessibilityIdentifier = "sculpture-remove-\(sculpture.slotIndex)"
        remove.accessibilityLabel = "Remove \(name.text ?? sculpture.catalogID)"
        MuseAccessibility.ensureMinimumTapTarget(remove)
        remove.addAction(UIAction { [weak self] _ in
            self?.remove(slotIndex: sculpture.slotIndex)
        }, for: .touchUpInside)

        let row = UIStackView(arrangedSubviews: [name, remove])
        row.spacing = 12
        MuseAccessibility.reflowForTextSize(row, on: self)
        return row
    }

    private func makeCatalogRow(_ entry: SculptureCatalogEntry) -> UIView {
        let name = UILabel()
        name.text = entry.displayName
        name.font = .preferredFont(forTextStyle: .body)
        name.adjustsFontForContentSizeCategory = true
        name.numberOfLines = 0

        let add = UIButton(configuration: .borderedProminent())
        add.configuration?.title = "Add"
        add.isEnabled = coordinator.hasCapacity && !coordinator.isEditing
        add.accessibilityIdentifier = "sculpture-add-\(entry.id)"
        add.accessibilityLabel = "Add \(entry.displayName)"
        MuseAccessibility.ensureMinimumTapTarget(add)
        add.addAction(UIAction { [weak self] _ in
            self?.add(catalogID: entry.id)
        }, for: .touchUpInside)

        let row = UIStackView(arrangedSubviews: [name, add])
        row.spacing = 12
        MuseAccessibility.reflowForTextSize(row, on: self)
        return row
    }

    // MARK: - Actions

    private func add(catalogID: String) {
        viewModel.show(notice: nil)
        Task { [weak self] in
            guard let self else { return }
            let outcome = await self.coordinator.add(catalogID: catalogID)
            self.applyOutcome(outcome)
        }
    }

    private func remove(slotIndex: Int) {
        viewModel.show(notice: nil)
        Task { [weak self] in
            guard let self else { return }
            let outcome = await self.coordinator.remove(slotIndex: slotIndex)
            self.applyOutcome(outcome)
        }
    }

    private func applyOutcome(_ outcome: SculptureEditOutcome) {
        switch outcome {
        case .applied:
            viewModel.show(notice: nil)
        case .rejected(let message), .failed(let message):
            viewModel.show(notice: message)
        }
        render()
    }

    @objc private func handleDone() {
        dismiss(animated: true) { [onFinished] in onFinished() }
    }

    // MARK: - Copy

    static let emptyCatalogMessage = SculptureManagementViewModel.emptyCatalogMessage
    static let catalogFailedMessage = SculptureManagementViewModel.catalogFailedMessage

    // MARK: - Test seam

    var testEntries: [SculptureCatalogEntry] { viewModel.entries }
    var testNotice: String? { viewModel.notice }
    var testCatalogMessages: [String] { catalogStack.arrangedSubviews.compactMap { ($0 as? UILabel)?.text } }
    var testPlacedRowCount: Int { placedStack.arrangedSubviews.filter { $0 is UIStackView }.count }
    var testIsLoaded: Bool { viewModel.state != .loading }
    func testAdd(catalogID: String) { add(catalogID: catalogID) }
    func testRemove(slotIndex: Int) { remove(slotIndex: slotIndex) }
    func testAddButtonEnabled(catalogID: String) -> Bool? {
        for row in catalogStack.arrangedSubviews {
            guard let row = row as? UIStackView else { continue }
            for case let button as UIButton in row.arrangedSubviews
            where button.accessibilityIdentifier == "sculpture-add-\(catalogID)" {
                return button.isEnabled
            }
        }
        return nil
    }
}
