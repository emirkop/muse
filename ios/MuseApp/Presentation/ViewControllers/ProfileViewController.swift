import UIKit

final class ProfileViewController: UIViewController {
    private let viewModel: ProfileViewModel
    private let onChangeAvatar: ((String?) -> Void)?

    private let avatarLabel = UILabel()
    private let displayNameField = UITextField()
    private let saveButton = UIButton(configuration: .filled())
    private let retryButton = UIButton(configuration: .bordered())
    private let changeAvatarButton = UIButton(configuration: .plain())
    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let statusLabel = UILabel()

    private var loadedProfile: Profile?

    init(viewModel: ProfileViewModel, onChangeAvatar: ((String?) -> Void)? = nil) {
        self.viewModel = viewModel
        self.onChangeAvatar = onChangeAvatar
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Profile"
        view.backgroundColor = .systemBackground
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
        avatarLabel.font = .museScaled(ofSize: 15, weight: .regular)
        avatarLabel.adjustsFontForContentSizeCategory = true
        avatarLabel.textColor = .secondaryLabel
        avatarLabel.textAlignment = .center
        avatarLabel.numberOfLines = 0

        displayNameField.font = .museScaled(ofSize: 17, weight: .regular)
        displayNameField.adjustsFontForContentSizeCategory = true
        displayNameField.textAlignment = .center
        displayNameField.borderStyle = .roundedRect
        displayNameField.placeholder = "Display name"
        displayNameField.accessibilityLabel = "Display name"
        displayNameField.isEnabled = viewModel.isEditable

        var saveConfiguration = UIButton.Configuration.filled()
        saveConfiguration.title = "Save"
        saveButton.configuration = saveConfiguration
        saveButton.addTarget(self, action: #selector(handleSaveTapped), for: .touchUpInside)
        saveButton.isHidden = !viewModel.isEditable

        var changeAvatarConfiguration = UIButton.Configuration.plain()
        changeAvatarConfiguration.title = "Change Avatar"
        changeAvatarButton.configuration = changeAvatarConfiguration
        changeAvatarButton.addTarget(self, action: #selector(handleChangeAvatarTapped), for: .touchUpInside)
        changeAvatarButton.isHidden = !viewModel.isEditable || onChangeAvatar == nil

        statusLabel.font = .museScaled(ofSize: 14, weight: .regular)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .secondaryLabel
        statusLabel.textAlignment = .center
        statusLabel.numberOfLines = 0

        displayNameField.translatesAutoresizingMaskIntoConstraints = false

        retryButton.configuration?.title = "Try Again"
        retryButton.titleLabel?.adjustsFontForContentSizeCategory = true
        retryButton.accessibilityIdentifier = "profile-retry"
        retryButton.isHidden = true
        retryButton.addAction(UIAction { [weak self] _ in
            Task { await self?.viewModel.load() }
        }, for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [avatarLabel, changeAvatarButton, displayNameField, saveButton, activityIndicator, statusLabel, retryButton])
        stack.axis = .vertical
        stack.spacing = 16
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -32),
            displayNameField.widthAnchor.constraint(greaterThanOrEqualToConstant: 240)
        ])
    }

    @objc private func handleSaveTapped() {
        Task { await viewModel.save(displayName: displayNameField.text ?? "") }
    }

    @objc private func handleChangeAvatarTapped() {
        let currentAvatarID = loadedProfile?.avatarID.isEmpty == false ? loadedProfile?.avatarID : nil
        onChangeAvatar?(currentAvatarID)
    }

    private func render(_ state: ProfileViewModel.State) {
        retryButton.isHidden = true
        switch state {
        case .loading:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Loading profile"
            saveButton.isEnabled = false
            statusLabel.text = nil
        case .loaded(let profile):
            loadedProfile = profile
            activityIndicator.stopAnimating()
            saveButton.isEnabled = true
            statusLabel.text = viewModel.refreshFailureNotice
            if let notice = viewModel.refreshFailureNotice {
                retryButton.isHidden = false
                MuseAccessibility.announceFailure(notice)
            }
            activityIndicator.isAccessibilityElement = false
            displayNameField.text = profile.displayName
            avatarLabel.text = profile.avatarID.isEmpty
                ? "No avatar selected yet."
                : "Current avatar: \(profile.avatarID)"
        case .failed(let message):
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            saveButton.isEnabled = true
            retryButton.isHidden = loadedProfile != nil
            statusLabel.text = message
            MuseAccessibility.announceFailure(message)
        }
    }
}
