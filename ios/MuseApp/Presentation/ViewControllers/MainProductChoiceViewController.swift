import UIKit

final class MainProductChoiceViewController: UIViewController {
    private let onSelectMuseum: () -> Void
    private let onSelectCollectionRooms: () -> Void
    private let onViewProfile: () -> Void

    init(
        onSelectMuseum: @escaping () -> Void,
        onSelectCollectionRooms: @escaping () -> Void,
        onViewProfile: @escaping () -> Void
    ) {
        self.onSelectMuseum = onSelectMuseum
        self.onSelectCollectionRooms = onSelectCollectionRooms
        self.onViewProfile = onViewProfile
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Muse"
        view.backgroundColor = .systemBackground
        configureLayout()
    }

    private func configureLayout() {
        let museumTile = makeTile(
            symbolName: "building.columns.fill",
            title: "Museum",
            subtitle: "Create Your Museum",
            tint: .systemIndigo,
            action: #selector(handleMuseumTapped)
        )
        let collectionRoomsTile = makeTile(
            symbolName: "square.grid.2x2.fill",
            title: "Collection Rooms",
            subtitle: "Create a Collection Room",
            tint: .systemTeal,
            action: #selector(handleCollectionRoomsTapped)
        )

        let tileRow = UIStackView(arrangedSubviews: [museumTile, collectionRoomsTile])
        tileRow.spacing = 16
        tileRow.distribution = .fillEqually
        MuseAccessibility.reflowForTextSize(tileRow, verticalAlignment: .fill, on: self)

        var profileConfiguration = UIButton.Configuration.plain()
        profileConfiguration.title = "Profile"
        let profileButton = UIButton(configuration: profileConfiguration)
        profileButton.addTarget(self, action: #selector(handleProfileTapped), for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [tileRow, profileButton])
        stack.axis = .vertical
        stack.spacing = 24
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

    private func makeTile(symbolName: String, title: String, subtitle: String, tint: UIColor, action: Selector) -> UIButton {
        var configuration = UIButton.Configuration.filled()
        configuration.baseBackgroundColor = tint
        configuration.image = UIImage(systemName: symbolName)
        configuration.imagePlacement = .top
        configuration.imagePadding = 8
        configuration.title = title
        configuration.subtitle = subtitle
        configuration.cornerStyle = .large

        let button = UIButton(configuration: configuration)
        button.addTarget(self, action: action, for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        button.heightAnchor.constraint(greaterThanOrEqualToConstant: 140).isActive = true
        button.accessibilityLabel = title
        button.accessibilityHint = subtitle
        return button
    }

    @objc private func handleMuseumTapped() {
        onSelectMuseum()
    }

    @objc private func handleCollectionRoomsTapped() {
        onSelectCollectionRooms()
    }

    @objc private func handleProfileTapped() {
        onViewProfile()
    }
}
