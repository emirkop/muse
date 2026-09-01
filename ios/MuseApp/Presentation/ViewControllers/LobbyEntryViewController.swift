import UIKit

final class LobbyEntryViewController: UIViewController {
    // MARK: - Properties

    private let viewModel: LobbyEntryViewModel
    private let onEnterLobby: (LobbyRuntimeContent) -> Void
    private let onEnterRoomDirectly: (Room) -> Void
    private let onManageRooms: () -> Void

    private let titleLabel = UILabel()
    private let summaryLabel = UILabel()
    private let messageLabel = UILabel()
    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let retryButton = UIButton(configuration: .filled())
    private let manageRoomsButton = UIButton(configuration: .plain())
    private var lastAnnouncedStateIdentity: String?

    // MARK: - Initialization

    init(
        viewModel: LobbyEntryViewModel,
        onEnterLobby: @escaping (LobbyRuntimeContent) -> Void,
        onEnterRoomDirectly: @escaping (Room) -> Void,
        onManageRooms: @escaping () -> Void
    ) {
        self.viewModel = viewModel
        self.onEnterLobby = onEnterLobby
        self.onEnterRoomDirectly = onEnterRoomDirectly
        self.onManageRooms = onManageRooms
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Museum"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        Task { await viewModel.load() }
    }

    private func configureLayout() {
        titleLabel.font = .museScaled(ofSize: 22, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        summaryLabel.font = .museScaled(ofSize: 15, weight: .medium)
        summaryLabel.adjustsFontForContentSizeCategory = true
        summaryLabel.textColor = .secondaryLabel
        summaryLabel.textAlignment = .center
        summaryLabel.numberOfLines = 0

        messageLabel.font = .museScaled(ofSize: 15)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .secondaryLabel
        messageLabel.textAlignment = .center
        messageLabel.numberOfLines = 0

        var retryConfiguration = UIButton.Configuration.filled()
        retryConfiguration.title = "Try Again"
        retryConfiguration.cornerStyle = .medium
        retryButton.configuration = retryConfiguration
        retryButton.addTarget(self, action: #selector(handleRetry), for: .touchUpInside)
        retryButton.translatesAutoresizingMaskIntoConstraints = false

        var manageConfiguration = UIButton.Configuration.plain()
        manageConfiguration.title = "Manage Rooms"
        manageRoomsButton.configuration = manageConfiguration
        manageRoomsButton.addTarget(self, action: #selector(handleManageRooms), for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [
            titleLabel, summaryLabel, activityIndicator, messageLabel, retryButton, manageRoomsButton
        ])
        stack.axis = .vertical
        stack.spacing = 14
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -32),
            retryButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 200)
        ])
        render(viewModel.state)
    }

    private var openingTitle: String {
        viewModel.isVisitor ? "Opening this Museum" : "Opening your Museum"
    }

    // MARK: - Rendering

    private func render(_ state: LobbyEntryViewModel.State) {
        retryButton.isHidden = true
        manageRoomsButton.isHidden = true
        activityIndicator.stopAnimating()
        summaryLabel.text = viewModel.contentSummary

        switch state {
        case .checking:
            titleLabel.text = openingTitle
            messageLabel.text = nil
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = openingTitle

        case .noVisibleRooms:
            titleLabel.text = viewModel.isVisitor ? "Nothing to see yet" : "No Rooms yet"
            messageLabel.text = viewModel.noVisibleRoomsMessage
            manageRoomsButton.isHidden = viewModel.isVisitor

        case .noLongerAvailable:
            titleLabel.text = "Link no longer available"
            messageLabel.text = viewModel.noLongerAvailableMessage
            summaryLabel.text = nil

        case .enterRoomDirectly(let room):
            titleLabel.text = "Entering…"
            messageLabel.text = nil
            onEnterRoomDirectly(room)

        case .lobbyDesignUnavailable:
            titleLabel.text = "Lobby not available yet"
            messageLabel.text = viewModel.designUnavailableMessage
            retryButton.isHidden = false
            manageRoomsButton.isHidden = viewModel.isVisitor

        case .placementUnresolvable(let failure):
            titleLabel.text = "Can't lay out this Lobby"
            messageLabel.text = viewModel.placementUnresolvableMessage(failure)
            retryButton.isHidden = false
            manageRoomsButton.isHidden = viewModel.isVisitor

        case .ready:
            titleLabel.text = "Entering…"
            messageLabel.text = nil
            if let content = viewModel.content {
                onEnterLobby(content)
            }

        case .failed(let message):
            titleLabel.text = viewModel.isVisitor ? "Couldn't open this Museum" : "Couldn't open your Museum"
            messageLabel.text = message
            retryButton.isHidden = false
            manageRoomsButton.isHidden = viewModel.isVisitor
        }

        announceStateIfChanged(state)
    }

    private func announceStateIfChanged(_ state: LobbyEntryViewModel.State) {
        let identity = String(String(describing: state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity
        activityIndicator.isAccessibilityElement = activityIndicator.isAnimating

        let message = [titleLabel.text, summaryLabel.text, messageLabel.text]
            .compactMap { $0 }
            .joined(separator: ". ")
        switch state {
        case .failed, .noLongerAvailable, .lobbyDesignUnavailable, .placementUnresolvable:
            MuseAccessibility.announceFailure(message)
        default:
            MuseAccessibility.announce(message)
        }
    }

    // MARK: - Actions

    @objc private func handleRetry() {
        Task { await viewModel.load() }
    }

    @objc private func handleManageRooms() {
        onManageRooms()
    }
}
