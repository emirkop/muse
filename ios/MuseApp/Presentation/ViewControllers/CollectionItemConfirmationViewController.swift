import UIKit

final class CollectionItemConfirmationViewController: UIViewController {
    private let viewModel: CollectionItemAdditionViewModel
    private let roomName: String
    private let onAdded: (CollectionRoom) -> Void
    var onCapacityReached: (() -> Void)?

    private let titleLabel = UILabel()
    private let detailLabel = UILabel()
    private let assetNoteLabel = UILabel()
    private let addButton = UIButton(configuration: .filled())
    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let outcomeLabel = UILabel()
    private var lastAnnouncedStateIdentity: String?

    init(
        viewModel: CollectionItemAdditionViewModel,
        roomName: String,
        onAdded: @escaping (CollectionRoom) -> Void
    ) {
        self.viewModel = viewModel
        self.roomName = roomName
        self.onAdded = onAdded
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Add Item"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in self?.render(state) }
        render(viewModel.state)
    }

    // MARK: - Layout

    private func configureLayout() {
        titleLabel.text = viewModel.modelDescription
        titleLabel.museMarkAsHeader()
        titleLabel.font = .preferredFont(forTextStyle: .title2)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.numberOfLines = 0

        detailLabel.text = "Add to \(roomName)"
        detailLabel.font = .preferredFont(forTextStyle: .body)
        detailLabel.adjustsFontForContentSizeCategory = true
        detailLabel.textColor = .secondaryLabel
        detailLabel.numberOfLines = 0

        assetNoteLabel.text = viewModel.assetNote
        assetNoteLabel.font = .preferredFont(forTextStyle: .footnote)
        assetNoteLabel.adjustsFontForContentSizeCategory = true
        assetNoteLabel.textColor = .tertiaryLabel
        assetNoteLabel.numberOfLines = 0

        outcomeLabel.font = .preferredFont(forTextStyle: .subheadline)
        outcomeLabel.adjustsFontForContentSizeCategory = true
        outcomeLabel.numberOfLines = 0
        outcomeLabel.isHidden = true

        addButton.setTitle("Add to Collection", for: .normal)
        addButton.accessibilityIdentifier = "collection-item-add"
        addButton.addTarget(self, action: #selector(handleAdd), for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [
            titleLabel, detailLabel, assetNoteLabel, outcomeLabel, addButton, activityIndicator
        ])
        stack.axis = .vertical
        stack.spacing = 16
        stack.alignment = .fill
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 32),
            stack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -24)
        ])
    }

    // MARK: - Actions

    @objc private func handleAdd() {
        if case .capacityReached = viewModel.state {
            onCapacityReached?()
            return
        }
        Task { await viewModel.confirm() }
    }

    // MARK: - Rendering

    private func render(_ state: CollectionItemAdditionViewModel.State) {
        switch state {
        case .confirming:
            activityIndicator.stopAnimating()
            addButton.isEnabled = true
            addButton.setTitle("Add to Collection", for: .normal)
            outcomeLabel.isHidden = true

        case .adding:
            activityIndicator.startAnimating()
            addButton.isEnabled = false
            outcomeLabel.isHidden = true

        case .added(let slotIndex, let spaceGrew, let geometryIncomplete):
            activityIndicator.stopAnimating()
            addButton.isEnabled = true
            addButton.setTitle("Add Another", for: .normal)
            outcomeLabel.textColor = .label
            var lines = ["Added to display slot \(slotIndex)."]
            if spaceGrew {
                lines.append("The room grew to fit it.")
            }
            if geometryIncomplete {
                lines.append("Some of the new space hasn't finished downloading.")
            }
            outcomeLabel.text = lines.joined(separator: " ")
            outcomeLabel.isHidden = false
            onAdded(viewModel.room)

        case .designUnavailable:
            activityIndicator.stopAnimating()
            addButton.isEnabled = false
            outcomeLabel.textColor = .secondaryLabel
            outcomeLabel.text = """
                This room's design isn't available yet, so there's nowhere to \
                put an item. Choose a design for the room first — and note that \
                no collection room design artwork has been produced yet.
                """
            outcomeLabel.isHidden = false

        case .failed(let message, let retryable):
            activityIndicator.stopAnimating()
            addButton.isEnabled = retryable
            addButton.setTitle(retryable ? "Try Again" : "Add to Collection", for: .normal)
            outcomeLabel.textColor = .systemRed
            outcomeLabel.text = message
            outcomeLabel.isHidden = false

        case .capacityReached:
            activityIndicator.stopAnimating()
            addButton.isEnabled = onCapacityReached != nil
            addButton.setTitle("See Upgrade Options", for: .normal)
            outcomeLabel.textColor = .label
            outcomeLabel.text = "You've reached your collection's item capacity. The item wasn't added."
            outcomeLabel.isHidden = false
        }

        announceOutcomeIfChanged(state)
    }

    private func announceOutcomeIfChanged(_ state: CollectionItemAdditionViewModel.State) {
        let identity = String(String(describing: state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity
        activityIndicator.isAccessibilityElement = activityIndicator.isAnimating

        guard !outcomeLabel.isHidden else { return }
        switch state {
        case .failed:
            MuseAccessibility.announceFailure(outcomeLabel.text)
        default:
            MuseAccessibility.announce(outcomeLabel.text)
        }
    }

    // MARK: - Test seam

    func testTapAdd() { handleAdd() }
    var testAddEnabled: Bool { addButton.isEnabled }
    var testOutcomeText: String? { outcomeLabel.isHidden ? nil : outcomeLabel.text }
    var testAddTitle: String? { addButton.title(for: .normal) }
}
