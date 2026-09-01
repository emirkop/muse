import UIKit

final class RoomMusicSelectionViewController: UIViewController {
    private let viewModel: RoomMusicSelectionViewModel
    private let onChanged: (String?) -> Void
    private let musicSession: RoomMusicSession?
    private var musicToggle: MusicToggleBarButton?
    private var musicTask: Task<Void, Never>?

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let messageLabel = UILabel()
    private let scrollView = UIScrollView()
    private let contentStack = UIStackView()

    init(
        viewModel: RoomMusicSelectionViewModel,
        musicSession: RoomMusicSession? = nil,
        onChanged: @escaping (String?) -> Void
    ) {
        self.viewModel = viewModel
        self.musicSession = musicSession
        self.onChanged = onChanged
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Room Music"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            guard let self else { return }
            self.render(state)
            if case .loaded(_, let assigned) = state {
                self.onChanged(assigned)
                if let session = self.musicSession {
                    self.musicTask = Task { [weak self] in
                        await session.roomTrackChanged(to: assigned)
                        self?.renderMusicToggle()
                    }
                }
            }
        }
        if let musicSession {
            let toggle = MusicToggleBarButton(session: musicSession)
            musicToggle = toggle
            musicSession.onStateChange = { [weak self] _ in self?.renderMusicToggle() }
            renderMusicToggle()
        }
        Task { await viewModel.load() }
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        if let musicSession {
            musicTask = Task { [weak self] in
                await musicSession.enterRoom()
                self?.renderMusicToggle()
            }
        }
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        musicSession?.leaveRoom()
    }

    private func renderMusicToggle() {
        musicToggle?.render()
        navigationItem.rightBarButtonItem = musicToggle?.item
    }

    private func configureLayout() {
        messageLabel.font = .museScaled(ofSize: 15)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .secondaryLabel
        messageLabel.textAlignment = .center
        messageLabel.numberOfLines = 0

        contentStack.axis = .vertical
        contentStack.spacing = 12
        contentStack.translatesAutoresizingMaskIntoConstraints = false
        scrollView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.addSubview(contentStack)
        view.addSubview(scrollView)

        activityIndicator.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(activityIndicator)

        NSLayoutConstraint.activate([
            scrollView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            scrollView.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor),
            scrollView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            contentStack.topAnchor.constraint(equalTo: scrollView.topAnchor, constant: 24),
            contentStack.bottomAnchor.constraint(equalTo: scrollView.bottomAnchor, constant: -24),
            contentStack.leadingAnchor.constraint(equalTo: scrollView.leadingAnchor, constant: 20),
            contentStack.trailingAnchor.constraint(equalTo: scrollView.trailingAnchor, constant: -20),
            contentStack.widthAnchor.constraint(equalTo: scrollView.widthAnchor, constant: -40),
            activityIndicator.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            activityIndicator.centerYAnchor.constraint(equalTo: view.centerYAnchor)
        ])
    }

    private func render(_ state: RoomMusicSelectionViewModel.State) {
        contentStack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        activityIndicator.stopAnimating()
        activityIndicator.isAccessibilityElement = false

        switch state {
        case .loading:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Loading music"

        case .catalogEmpty:
            messageLabel.text = viewModel.catalogEmptyMessage
            contentStack.addArrangedSubview(messageLabel)

        case .loaded(let tracks, let assigned):
            if assigned != nil {
                contentStack.addArrangedSubview(makeRemoveRow())
            }
            for track in tracks {
                contentStack.addArrangedSubview(makeTrackRow(track, isAssigned: track.id == assigned))
            }

        case .failed(let message, let tracks):
            messageLabel.text = message
            contentStack.addArrangedSubview(messageLabel)
            if tracks.isEmpty {
                contentStack.addArrangedSubview(makeRetryButton())
            } else {
                for track in tracks {
                    contentStack.addArrangedSubview(makeTrackRow(track, isAssigned: track.id == viewModel.assignedTrackID))
                }
            }
            MuseAccessibility.announceFailure(message)
        }

        MuseAccessibility.announceLayoutChange()
    }

    private func makeTrackRow(_ track: MusicTrack, isAssigned: Bool) -> UIView {
        var configuration = UIButton.Configuration.bordered()
        configuration.title = isAssigned ? "\(track.displayName)  ✓" : track.displayName
        configuration.subtitle = viewModel.subtitle(for: track)
        configuration.cornerStyle = .medium
        let button = UIButton(configuration: configuration)
        button.accessibilityIdentifier = "music-track-\(track.id)"
        button.accessibilityLabel = track.displayName
        if isAssigned {
            button.accessibilityTraits = [.button, .selected]
            button.accessibilityValue = "Currently assigned"
        }
        button.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.assign(trackID: track.id) }
        }, for: .touchUpInside)
        return button
    }

    private func makeRemoveRow() -> UIView {
        var configuration = UIButton.Configuration.plain()
        configuration.title = "No Music"
        configuration.subtitle = "This Room plays nothing"
        let button = UIButton(configuration: configuration)
        button.accessibilityIdentifier = "music-remove"
        button.accessibilityLabel = "No music"
        button.accessibilityHint = "Removes this Room's music"
        button.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.removeMusic() }
        }, for: .touchUpInside)
        return button
    }

    private func makeRetryButton() -> UIButton {
        var configuration = UIButton.Configuration.bordered()
        configuration.title = "Try Again"
        configuration.cornerStyle = .medium
        let button = UIButton(configuration: configuration)
        button.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.load() }
        }, for: .touchUpInside)
        return button
    }
}

// MARK: - Test seams

extension RoomMusicSelectionViewController {
    var testVisibleText: [String] {
        contentStack.arrangedSubviews.compactMap {
            ($0 as? UILabel)?.text ?? ($0 as? UIButton)?.configuration?.title
        }
    }

    var testHasUploadAffordance: Bool {
        testVisibleText.contains { $0.localizedCaseInsensitiveContains("upload") }
    }

    var testMusicSession: RoomMusicSession? { musicSession }
    var testMusicToggleItem: UIBarButtonItem? { navigationItem.rightBarButtonItem }
    func testAwaitMusic() async { await musicTask?.value }
}
