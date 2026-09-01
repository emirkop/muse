import UIKit

final class RoomListViewController: UIViewController {
    // MARK: - Properties

    private let viewModel: RoomListViewModel
    private let onCreateRoom: () -> Void
    private let onSelectRoom: (Room) -> Void
    private let onAddPhotos: (Room) -> Void
    private let onEnterRoom: (Room) -> Void
    private let onEnterLobby: () -> Void
    private let onOpenRuntimeSkeleton: () -> Void

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let messageLabel = UILabel()
    private let roomStack = UIStackView()
    private let createButton = UIButton(configuration: .filled())
    private let enterLobbyButton = UIButton(configuration: .bordered())
    private let retryButton = UIButton(configuration: .bordered())
    private let refreshNoticeLabel = UILabel()

    // MARK: - Initialization

    init(
        viewModel: RoomListViewModel,
        onCreateRoom: @escaping () -> Void,
        onSelectRoom: @escaping (Room) -> Void,
        onAddPhotos: @escaping (Room) -> Void,
        onEnterRoom: @escaping (Room) -> Void,
        onEnterLobby: @escaping () -> Void,
        onOpenRuntimeSkeleton: @escaping () -> Void
    ) {
        self.viewModel = viewModel
        self.onCreateRoom = onCreateRoom
        self.onSelectRoom = onSelectRoom
        self.onAddPhotos = onAddPhotos
        self.onEnterRoom = onEnterRoom
        self.onEnterLobby = onEnterLobby
        self.onOpenRuntimeSkeleton = onOpenRuntimeSkeleton
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Rooms"
        view.backgroundColor = .systemBackground
        navigationItem.rightBarButtonItem = UIBarButtonItem(
            title: "3D",
            style: .plain,
            target: self,
            action: #selector(handleRuntimeSkeletonTapped)
        )
        navigationItem.rightBarButtonItem?.accessibilityLabel = "Open 3D runtime skeleton"
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
        messageLabel.font = .museScaled(ofSize: 15, weight: .regular)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .secondaryLabel
        messageLabel.textAlignment = .center
        messageLabel.numberOfLines = 0

        roomStack.axis = .vertical
        roomStack.spacing = 8
        roomStack.alignment = .center

        var createConfiguration = UIButton.Configuration.filled()
        createConfiguration.title = "+ New Room"
        createConfiguration.cornerStyle = .medium
        createButton.configuration = createConfiguration
        createButton.addTarget(self, action: #selector(handleCreateTapped), for: .touchUpInside)
        createButton.translatesAutoresizingMaskIntoConstraints = false

        var lobbyConfiguration = UIButton.Configuration.bordered()
        lobbyConfiguration.title = "Enter Museum"
        lobbyConfiguration.subtitle = "Lobby"
        lobbyConfiguration.cornerStyle = .medium
        enterLobbyButton.configuration = lobbyConfiguration
        enterLobbyButton.addTarget(self, action: #selector(handleEnterLobbyTapped), for: .touchUpInside)
        enterLobbyButton.translatesAutoresizingMaskIntoConstraints = false

        retryButton.configuration?.title = "Try Again"
        retryButton.titleLabel?.adjustsFontForContentSizeCategory = true
        retryButton.accessibilityIdentifier = "room-list-retry"
        retryButton.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.load() }
        }, for: .touchUpInside)

        refreshNoticeLabel.font = .preferredFont(forTextStyle: .footnote)
        refreshNoticeLabel.adjustsFontForContentSizeCategory = true
        refreshNoticeLabel.textColor = .secondaryLabel
        refreshNoticeLabel.numberOfLines = 0
        refreshNoticeLabel.textAlignment = .center
        refreshNoticeLabel.accessibilityIdentifier = "room-list-refresh-notice"

        let stack = UIStackView(arrangedSubviews: [
            activityIndicator, messageLabel, refreshNoticeLabel, retryButton,
            enterLobbyButton, roomStack, createButton
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
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -24),
            createButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 240),
            enterLobbyButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 240)
        ])
    }

    // MARK: - Rendering

    private func render(_ state: RoomListViewModel.State) {
        roomStack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        createButton.isHidden = !viewModel.canCreateRoom
        retryButton.isHidden = true
        refreshNoticeLabel.text = viewModel.refreshFailureNotice
        refreshNoticeLabel.isHidden = viewModel.refreshFailureNotice == nil

        switch state {
        case .loading:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Loading your Rooms"
            messageLabel.text = nil

        case .loaded(let rooms):
            activityIndicator.stopAnimating()
            messageLabel.text = rooms.isEmpty
                ? "Your Museum has no Rooms yet."
                : "\(rooms.count) Room\(rooms.count == 1 ? "" : "s")"
            for room in rooms {
                roomStack.addArrangedSubview(makeRoomRow(room))
            }

        case .failed(let message):
            activityIndicator.stopAnimating()
            messageLabel.text = message
            retryButton.isHidden = false
            MuseAccessibility.announceFailure(message)
        }

        activityIndicator.isAccessibilityElement = false
        MuseAccessibility.announceLayoutChange(focusing: messageLabel)
    }

    private func makeRoomRow(_ room: Room) -> UIView {
        var configuration = UIButton.Configuration.bordered()
        configuration.title = room.name
        configuration.subtitle = "\(room.photoSlots.count) of \(Room.maxPhotos) photos · tap to change design"
        configuration.cornerStyle = .medium

        let row = UIButton(configuration: configuration)
        row.addAction(UIAction { [weak self] _ in self?.onSelectRoom(room) }, for: .touchUpInside)
        row.translatesAutoresizingMaskIntoConstraints = false

        var photosConfiguration = UIButton.Configuration.plain()
        photosConfiguration.title = room.hasCapacityForPhoto ? "Add Photos" : "Room Full"
        photosConfiguration.buttonSize = .small
        let photosButton = UIButton(configuration: photosConfiguration)
        photosButton.isEnabled = room.hasCapacityForPhoto
        photosButton.accessibilityLabel = room.hasCapacityForPhoto
            ? "Add photos to \(room.name)"
            : "\(room.name) is full"
        MuseAccessibility.ensureMinimumTapTarget(photosButton)
        photosButton.addAction(UIAction { [weak self] _ in self?.onAddPhotos(room) }, for: .touchUpInside)

        var enterConfiguration = UIButton.Configuration.plain()
        enterConfiguration.title = "Enter Room"
        enterConfiguration.buttonSize = .small
        let enterButton = UIButton(configuration: enterConfiguration)
        enterButton.accessibilityLabel = "Enter \(room.name)"
        MuseAccessibility.ensureMinimumTapTarget(enterButton)
        enterButton.addAction(UIAction { [weak self] _ in self?.onEnterRoom(room) }, for: .touchUpInside)

        let actions = UIStackView(arrangedSubviews: [enterButton, photosButton])
        actions.spacing = 12
        MuseAccessibility.reflowForTextSize(actions, on: self)

        let column = UIStackView(arrangedSubviews: [row, actions])
        column.axis = .vertical
        column.spacing = 2
        column.alignment = .center
        column.translatesAutoresizingMaskIntoConstraints = false
        row.widthAnchor.constraint(greaterThanOrEqualToConstant: 260).isActive = true
        row.accessibilityLabel = room.name
        row.accessibilityHint = "Changes this Room's design"
        return column
    }

    // MARK: - Actions

    @objc private func handleCreateTapped() {
        onCreateRoom()
    }

    @objc private func handleEnterLobbyTapped() {
        onEnterLobby()
    }

    @objc private func handleRuntimeSkeletonTapped() {
        onOpenRuntimeSkeleton()
    }
}
