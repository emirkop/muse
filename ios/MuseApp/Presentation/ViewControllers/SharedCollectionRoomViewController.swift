import UIKit

final class SharedCollectionRoomViewController: UIViewController {
    private let content: SharedCollectionRoomContent
    private let musicSession: RoomMusicSession?
    private var musicToggle: MusicToggleBarButton?
    private var musicTask: Task<Void, Never>?

    init(
        content: SharedCollectionRoomContent,
        musicCatalog: (any MusicCatalogServicing)? = nil,
        musicPlayer: (any RoomMusicPlaying)? = nil,
        accessToken: String? = nil
    ) {
        self.content = content
        if let trackID = content.musicTrackID, let musicCatalog, let musicPlayer, let accessToken {
            musicSession = RoomMusicSession(trackID: trackID, catalog: musicCatalog, player: musicPlayer, accessToken: accessToken)
        } else {
            musicSession = nil
        }
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = content.name
        view.backgroundColor = .systemBackground
        if let musicSession {
            let toggle = MusicToggleBarButton(session: musicSession)
            musicToggle = toggle
            musicSession.onStateChange = { [weak self] _ in self?.renderMusicToggle() }
            renderMusicToggle()
        }

        let roleLabel = makeLabel("Visiting as a guest — view only", size: 13, color: .secondaryLabel)
        let nameLabel = makeLabel(content.name, size: 22, weight: .semibold)
        nameLabel.museMarkAsHeader()
        let scopeLabel = makeLabel(
            [
                content.categoryID.map { "Category: \($0)" },
                content.designID.map { "Design: \($0)" },
                "Tier: \(content.currentTier.ordinal)"
            ].compactMap { $0 }.joined(separator: "\n"),
            size: 15, color: .secondaryLabel
        )
        let itemsHeading = makeLabel(
            content.items.isEmpty ? "No items placed yet." : "Items (\(content.items.count))",
            size: 17, weight: .semibold
        )
        itemsHeading.museMarkAsHeader()
        let itemsStack = UIStackView(arrangedSubviews: content.items
            .sorted { $0.slotIndex < $1.slotIndex }
            .map { makeLabel("Slot \($0.slotIndex): \($0.catalogModelID)", size: 15) })
        itemsStack.axis = .vertical
        itemsStack.spacing = 6
        itemsStack.alignment = .leading

        let placeholderLabel = makeLabel(
            "The 3D Collection Room isn't available yet — its design and item artwork haven't been authored.",
            size: 13, color: .tertiaryLabel
        )

        let stack = UIStackView(arrangedSubviews: [roleLabel, nameLabel, scopeLabel, itemsHeading, itemsStack, placeholderLabel])
        stack.axis = .vertical
        stack.spacing = 14
        stack.alignment = .leading
        stack.translatesAutoresizingMaskIntoConstraints = false

        let scrollView = UIScrollView()
        scrollView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.addSubview(stack)
        view.addSubview(scrollView)

        NSLayoutConstraint.activate([
            scrollView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            scrollView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            scrollView.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor),
            stack.topAnchor.constraint(equalTo: scrollView.topAnchor, constant: 24),
            stack.leadingAnchor.constraint(equalTo: scrollView.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(equalTo: scrollView.trailingAnchor, constant: -24),
            stack.bottomAnchor.constraint(equalTo: scrollView.bottomAnchor, constant: -24),
            stack.widthAnchor.constraint(equalTo: scrollView.widthAnchor, constant: -48)
        ])
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

    private func makeLabel(_ text: String, size: CGFloat, weight: UIFont.Weight = .regular, color: UIColor = .label) -> UILabel {
        let label = UILabel()
        label.text = text
        label.font = .museScaled(ofSize: size, weight: weight)
        label.adjustsFontForContentSizeCategory = true
        label.textColor = color
        label.numberOfLines = 0
        return label
    }
}

// MARK: - Test seams

extension SharedCollectionRoomViewController {
    var testContent: SharedCollectionRoomContent { content }
    var testMusicSession: RoomMusicSession? { musicSession }
    var testMusicToggleItem: UIBarButtonItem? { navigationItem.rightBarButtonItem }
    func testAwaitMusic() async { await musicTask?.value }
}
