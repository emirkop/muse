import UIKit

final class AvatarSelectionViewController: UIViewController {
    private let viewModel: AvatarSelectionViewModel
    private let currentAvatarID: String?
    private let onCompleted: (String) -> Void

    private var selectedAvatarID: String?
    private var avatarButtons: [String: UIButton] = [:]
    private let confirmButton = UIButton(configuration: .filled())
    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let statusLabel = UILabel()

    init(viewModel: AvatarSelectionViewModel, currentAvatarID: String?, onCompleted: @escaping (String) -> Void) {
        self.viewModel = viewModel
        self.currentAvatarID = currentAvatarID
        self.selectedAvatarID = currentAvatarID
        self.onCompleted = onCompleted
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = currentAvatarID == nil ? "Choose Your Avatar" : "Change Avatar"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        updateConfirmButtonEnabled()
    }

    private func configureLayout() {
        let promptLabel = UILabel()
        promptLabel.text = "Pick one of the five predefined avatars."
        promptLabel.font = .museScaled(ofSize: 15, weight: .regular)
        promptLabel.adjustsFontForContentSizeCategory = true
        promptLabel.textColor = .secondaryLabel
        promptLabel.textAlignment = .center
        promptLabel.numberOfLines = 0
        promptLabel.museMarkAsHeader()

        let avatarRow = UIStackView(arrangedSubviews: AvatarCatalog.all.map(makeAvatarButton))
        avatarRow.spacing = 10
        avatarRow.distribution = .fillEqually
        MuseAccessibility.reflowForTextSize(avatarRow, verticalAlignment: .fill, on: self)

        var confirmConfiguration = UIButton.Configuration.filled()
        confirmConfiguration.title = "Choose This Avatar"
        confirmButton.configuration = confirmConfiguration
        confirmButton.addAction(UIAction { [weak self] _ in self?.handleConfirmTapped() }, for: .touchUpInside)

        statusLabel.font = .museScaled(ofSize: 14, weight: .regular)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .secondaryLabel
        statusLabel.textAlignment = .center
        statusLabel.numberOfLines = 0

        confirmButton.translatesAutoresizingMaskIntoConstraints = false
        avatarRow.translatesAutoresizingMaskIntoConstraints = false

        let stack = UIStackView(arrangedSubviews: [promptLabel, avatarRow, confirmButton, activityIndicator, statusLabel])
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
            avatarRow.widthAnchor.constraint(lessThanOrEqualTo: view.widthAnchor, constant: -48),
            confirmButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 220)
        ])
    }

    private func makeAvatarButton(for avatar: Avatar) -> UIButton {
        var configuration = UIButton.Configuration.plain()
        configuration.image = UIImage(systemName: "person.crop.circle.fill")
        configuration.imagePlacement = .top
        configuration.imagePadding = 6
        configuration.title = avatar.displayName
        configuration.baseForegroundColor = .label
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 8, leading: 4, bottom: 8, trailing: 4)

        let button = UIButton(configuration: configuration)
        button.layer.cornerRadius = 8
        button.addAction(UIAction { [weak self] _ in self?.handleAvatarTapped(avatar.id) }, for: .touchUpInside)
        button.accessibilityLabel = avatar.displayName
        MuseAccessibility.ensureMinimumTapTarget(button)
        avatarButtons[avatar.id] = button
        applyHighlight(to: button, isSelected: avatar.id == selectedAvatarID)
        return button
    }

    private func handleAvatarTapped(_ avatarID: String) {
        selectedAvatarID = avatarID
        for (id, button) in avatarButtons {
            applyHighlight(to: button, isSelected: id == avatarID)
        }
        updateConfirmButtonEnabled()
    }

    private func applyHighlight(to button: UIButton, isSelected: Bool) {
        button.layer.borderWidth = isSelected ? 2 : 0
        button.layer.borderColor = UIColor.tintColor.cgColor
        button.accessibilityTraits = isSelected ? [.button, .selected] : [.button]
    }

    private func updateConfirmButtonEnabled() {
        confirmButton.isEnabled = selectedAvatarID != nil
    }

    private func handleConfirmTapped() {
        guard let selectedAvatarID else { return }
        Task { await viewModel.selectAvatar(selectedAvatarID) }
    }

    private func render(_ state: AvatarSelectionViewModel.State) {
        switch state {
        case .idle:
            activityIndicator.stopAnimating()
            updateConfirmButtonEnabled()
            statusLabel.text = nil
        case .saving:
            activityIndicator.startAnimating()
            confirmButton.isEnabled = false
            statusLabel.text = nil
            MuseAccessibility.announce("Saving your avatar")
        case .saved(let avatarID):
            activityIndicator.stopAnimating()
            onCompleted(avatarID)
        case .failed(let message):
            activityIndicator.stopAnimating()
            updateConfirmButtonEnabled()
            statusLabel.text = message
            MuseAccessibility.announceFailure(message)
        }
    }
}

// MARK: - Test seam

extension AvatarSelectionViewController {
    func testCompleteSelection(avatarID: String) {
        onCompleted(avatarID)
    }
}
