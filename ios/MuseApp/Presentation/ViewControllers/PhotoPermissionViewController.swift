import UIKit

final class PhotoPermissionViewController: UIViewController {
    private let viewModel: PhotoPermissionViewModel
    private let limitedLibraryManager: LimitedPhotoLibraryManaging
    private let onProceedToSelection: () -> Void
    private let onContinueWithoutPhotos: () -> Void

    private let titleLabel = UILabel()
    private let bodyLabel = UILabel()
    private let noticeLabel = UILabel()
    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let primaryButton = UIButton(configuration: .filled())
    private let secondaryButton = UIButton(configuration: .plain())
    private let tertiaryButton = UIButton(configuration: .plain())

    init(
        viewModel: PhotoPermissionViewModel,
        limitedLibraryManager: LimitedPhotoLibraryManaging = LimitedPhotoLibraryManager(),
        onProceedToSelection: @escaping () -> Void,
        onContinueWithoutPhotos: @escaping () -> Void
    ) {
        self.viewModel = viewModel
        self.limitedLibraryManager = limitedLibraryManager
        self.onProceedToSelection = onProceedToSelection
        self.onContinueWithoutPhotos = onContinueWithoutPhotos
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Photos"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }

        NotificationCenter.default.addObserver(
            self,
            selector: #selector(handleDidBecomeActive),
            name: UIApplication.didBecomeActiveNotification,
            object: nil
        )

        Task { await viewModel.start() }
    }

    private func configureLayout() {
        titleLabel.font = .museScaled(ofSize: 22, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textColor = .label
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        bodyLabel.font = .museScaled(ofSize: 15)
        bodyLabel.adjustsFontForContentSizeCategory = true
        bodyLabel.textColor = .secondaryLabel
        bodyLabel.textAlignment = .center
        bodyLabel.numberOfLines = 0

        noticeLabel.font = .museScaled(ofSize: 12)
        noticeLabel.adjustsFontForContentSizeCategory = true
        noticeLabel.textColor = .tertiaryLabel
        noticeLabel.textAlignment = .center
        noticeLabel.numberOfLines = 0

        primaryButton.addTarget(self, action: #selector(handlePrimaryTapped), for: .touchUpInside)
        secondaryButton.addTarget(self, action: #selector(handleSecondaryTapped), for: .touchUpInside)
        tertiaryButton.addTarget(self, action: #selector(handleTertiaryTapped), for: .touchUpInside)

        for button in [primaryButton, secondaryButton, tertiaryButton] {
            button.translatesAutoresizingMaskIntoConstraints = false
            button.widthAnchor.constraint(greaterThanOrEqualToConstant: 260).isActive = true
        }

        let stack = UIStackView(arrangedSubviews: [
            titleLabel, bodyLabel, activityIndicator,
            primaryButton, secondaryButton, noticeLabel, tertiaryButton
        ])
        stack.axis = .vertical
        stack.spacing = 14
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -32)
        ])
    }

    private var lastAnnouncedStateIdentity: String?

    // MARK: - Rendering

    private func render(_ state: PhotoPermissionViewModel.State) {
        activityIndicator.stopAnimating()
        primaryButton.isHidden = false
        secondaryButton.isHidden = true
        tertiaryButton.isHidden = false
        noticeLabel.isHidden = true

        tertiaryButton.configuration?.title = PhotoPermissionViewModel.continueWithoutPhotosActionTitle
        tertiaryButton.isHidden = !viewModel.allowsContinuingWithoutPhotos

        switch state {
        case .checking, .requesting:
            titleLabel.text = nil
            bodyLabel.text = nil
            primaryButton.isHidden = true
            tertiaryButton.isHidden = true
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Checking photo access"

        case .explaining:
            titleLabel.text = PhotoPermissionViewModel.explanationTitle
            bodyLabel.text = PhotoPermissionViewModel.explanationBody
            primaryButton.configuration?.title = PhotoPermissionViewModel.allowActionTitle

        case .granted(.limitedAccess):
            titleLabel.text = PhotoPermissionViewModel.limitedTitle
            bodyLabel.text = PhotoPermissionViewModel.limitedBody
            primaryButton.configuration?.title = "Choose Photos"
            secondaryButton.isHidden = !viewModel.showsManageSelectedPhotos
            secondaryButton.configuration?.title = PhotoPermissionViewModel.manageSelectionActionTitle

        case .granted:
            titleLabel.text = PhotoPermissionViewModel.fullAccessTitle
            bodyLabel.text = PhotoPermissionViewModel.fullAccessBody
            primaryButton.configuration?.title = "Choose Photos"

        case .denied:
            titleLabel.text = PhotoPermissionViewModel.deniedTitle
            bodyLabel.text = PhotoPermissionViewModel.deniedBody
            primaryButton.configuration?.title = PhotoPermissionViewModel.settingsActionTitle
            primaryButton.isHidden = !viewModel.showsSettingsLink
            noticeLabel.isHidden = false
            noticeLabel.text = PhotoPermissionViewModel.photolessRoomNotice

        case .restricted:
            titleLabel.text = PhotoPermissionViewModel.restrictedTitle
            bodyLabel.text = PhotoPermissionViewModel.restrictedBody
            primaryButton.isHidden = true
            noticeLabel.isHidden = false
            noticeLabel.text = PhotoPermissionViewModel.photolessRoomNotice
        }

        announceStateIfChanged(state)
    }

    private func announceStateIfChanged(_ state: PhotoPermissionViewModel.State) {
        let identity = String(String(describing: state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity
        activityIndicator.isAccessibilityElement = activityIndicator.isAnimating
        MuseAccessibility.announceScreenChange(focusing: titleLabel.text == nil ? nil : titleLabel)
    }

    // MARK: - Actions

    @objc private func handlePrimaryTapped() {
        switch viewModel.state {
        case .explaining:
            Task { await viewModel.requestAccess() }
        case .granted:
            onProceedToSelection()
        case .denied:
            openSettings()
        case .checking, .requesting, .restricted:
            break
        }
    }

    @objc private func handleSecondaryTapped() {
        guard viewModel.showsManageSelectedPhotos else { return }
        limitedLibraryManager.presentLimitedLibraryPicker(from: self)
    }

    @objc private func handleTertiaryTapped() {
        onContinueWithoutPhotos()
    }

    @objc private func handleDidBecomeActive() {
        Task { await viewModel.refresh() }
    }

    private func openSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString),
              UIApplication.shared.canOpenURL(url) else { return }
        UIApplication.shared.open(url)
    }
}
