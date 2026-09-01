import UIKit

final class CollectionRoomListViewController: UIViewController {
    // MARK: - Properties

    private let viewModel: CollectionRoomListViewModel
    private let onCreate: () -> Void
    private let onSelectRoom: (CollectionRoom) -> Void
    private let onAddItem: (CollectionRoom) -> Void
    private let onShare: (CollectionRoom) -> Void
    private let onMusic: (CollectionRoom) -> Void

    private let scrollView = UIScrollView()
    private let contentStack = UIStackView()
    private let statusLabel = UILabel()
    private let loadingIndicator = UIActivityIndicatorView(style: .medium)
    private let retryButton = UIButton(configuration: .bordered())
    private let roomsStack = UIStackView()
    private let createButton = UIButton(configuration: .filled())

    // MARK: - Initialization

    init(
        viewModel: CollectionRoomListViewModel,
        onCreate: @escaping () -> Void,
        onSelectRoom: @escaping (CollectionRoom) -> Void,
        onAddItem: @escaping (CollectionRoom) -> Void,
        onShare: @escaping (CollectionRoom) -> Void,
        onMusic: @escaping (CollectionRoom) -> Void
    ) {
        self.viewModel = viewModel
        self.onCreate = onCreate
        self.onSelectRoom = onSelectRoom
        self.onAddItem = onAddItem
        self.onShare = onShare
        self.onMusic = onMusic
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Collection Rooms"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        render(viewModel.state)
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        Task { await viewModel.load() }
    }

    private func configureLayout() {
        statusLabel.font = .museScaled(ofSize: 15)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .secondaryLabel
        statusLabel.numberOfLines = 0
        statusLabel.textAlignment = .center

        loadingIndicator.hidesWhenStopped = true
        loadingIndicator.accessibilityLabel = "Loading your Collection Rooms"

        retryButton.setTitle("Try Again", for: .normal)
        retryButton.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.load() }
        }, for: .touchUpInside)

        roomsStack.axis = .vertical
        roomsStack.spacing = 10

        createButton.setTitle("+ New Collection Room", for: .normal)
        createButton.addAction(UIAction { [weak self] _ in self?.onCreate() }, for: .touchUpInside)

        contentStack.axis = .vertical
        contentStack.spacing = 16
        contentStack.translatesAutoresizingMaskIntoConstraints = false
        [loadingIndicator, statusLabel, retryButton, roomsStack, createButton]
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

    private func render(_ state: CollectionRoomListViewModel.State) {
        roomsStack.arrangedSubviews.forEach { $0.removeFromSuperview() }

        switch state {
        case .loading:
            loadingIndicator.startAnimating()
            loadingIndicator.isAccessibilityElement = true
            statusLabel.isHidden = true
            retryButton.isHidden = true

        case .empty:
            loadingIndicator.stopAnimating()
            statusLabel.text = CollectionRoomListViewModel.emptyMessage
            statusLabel.isHidden = false
            retryButton.isHidden = true

        case .loaded(let rooms):
            loadingIndicator.stopAnimating()
            if let notice = viewModel.refreshFailureNotice {
                statusLabel.text = notice
                statusLabel.isHidden = false
                retryButton.isHidden = false
                MuseAccessibility.announceFailure(notice)
            } else {
                statusLabel.isHidden = true
                retryButton.isHidden = true
            }
            rooms.forEach { roomsStack.addArrangedSubview(makeRow(for: $0)) }

        case .failed(let message):
            loadingIndicator.stopAnimating()
            statusLabel.text = message
            statusLabel.isHidden = false
            retryButton.isHidden = false
            MuseAccessibility.announceFailure(message)
        }

        loadingIndicator.isAccessibilityElement = loadingIndicator.isAnimating
        MuseAccessibility.announceLayoutChange()
    }

    private func makeRow(for room: CollectionRoom) -> UIView {
        var configuration = UIButton.Configuration.plain()
        configuration.title = room.name
        configuration.subtitle = viewModel.categoryName(for: room)
        configuration.image = UIImage(systemName: "square.grid.2x2")
        configuration.imagePadding = 12
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 14, leading: 16, bottom: 14, trailing: 16)
        configuration.background.cornerRadius = 12
        configuration.background.strokeColor = .separator
        configuration.background.strokeWidth = 1

        let button = UIButton(configuration: configuration)
        button.contentHorizontalAlignment = .leading
        button.addAction(UIAction { [weak self] _ in self?.onSelectRoom(room) }, for: .touchUpInside)

        button.accessibilityLabel = room.name
        button.accessibilityHint = "Opens this Collection Room's design"

        let row = UIStackView(arrangedSubviews: [button, makeAddItemButton(for: room), makeMusicButton(for: room), makeShareButton(for: room)])
        row.spacing = 8
        MuseAccessibility.reflowForTextSize(row, verticalAlignment: .fill, on: self)
        return row
    }

    private func makeAddItemButton(for room: CollectionRoom) -> UIButton {
        var configuration = UIButton.Configuration.bordered()
        configuration.image = UIImage(systemName: "plus.magnifyingglass")
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 12, leading: 12, bottom: 12, trailing: 12)
        configuration.background.cornerRadius = 12

        let button = UIButton(configuration: configuration)
        button.accessibilityLabel = "Add item to \(room.name)"
        MuseAccessibility.ensureMinimumTapTarget(button)
        button.setContentCompressionResistancePriority(.required, for: .horizontal)
        button.addAction(UIAction { [weak self] _ in self?.onAddItem(room) }, for: .touchUpInside)
        return button
    }

    private func makeMusicButton(for room: CollectionRoom) -> UIButton {
        var configuration = UIButton.Configuration.bordered()
        configuration.image = UIImage(systemName: room.hasMusic ? "music.note" : "music.note.list")
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 12, leading: 12, bottom: 12, trailing: 12)
        configuration.background.cornerRadius = 12

        let button = UIButton(configuration: configuration)
        button.accessibilityLabel = "Music for \(room.name)"
        button.accessibilityValue = room.hasMusic ? "Music assigned" : "No music assigned"
        MuseAccessibility.ensureMinimumTapTarget(button)
        button.setContentCompressionResistancePriority(.required, for: .horizontal)
        button.addAction(UIAction { [weak self] _ in self?.onMusic(room) }, for: .touchUpInside)
        return button
    }

    private func makeShareButton(for room: CollectionRoom) -> UIButton {
        var configuration = UIButton.Configuration.bordered()
        configuration.image = UIImage(systemName: "square.and.arrow.up")
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 12, leading: 12, bottom: 12, trailing: 12)
        configuration.background.cornerRadius = 12

        let button = UIButton(configuration: configuration)
        button.accessibilityLabel = "Share \(room.name)"
        MuseAccessibility.ensureMinimumTapTarget(button)
        button.setContentCompressionResistancePriority(.required, for: .horizontal)
        button.addAction(UIAction { [weak self] _ in self?.onShare(room) }, for: .touchUpInside)
        return button
    }
}
