import UIKit

final class RoomCreationViewController: UIViewController {
    private let viewModel: RoomCreationViewModel
    private let onNameConfirmed: (String) -> Void

    private let nameField = UITextField()
    private let characterCountLabel = UILabel()
    private let validationLabel = UILabel()
    private let zeroPhotoNoticeLabel = UILabel()
    private let continueButton = UIButton(configuration: .filled())
    private var lastAnnouncedValidationMessage: String?

    init(viewModel: RoomCreationViewModel, onNameConfirmed: @escaping (String) -> Void) {
        self.viewModel = viewModel
        self.onNameConfirmed = onNameConfirmed
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "New Room"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.setStateChangeHandler { [weak self] in
            self?.renderNameFeedback()
        }
    }

    private func configureLayout() {
        let promptLabel = UILabel()
        promptLabel.text = "Name your Room"
        promptLabel.font = .museScaled(ofSize: 22, weight: .semibold)
        promptLabel.adjustsFontForContentSizeCategory = true
        promptLabel.textColor = .label
        promptLabel.textAlignment = .center
        promptLabel.numberOfLines = 0
        promptLabel.museMarkAsHeader()

        nameField.placeholder = RoomNamingRules.placeholderExample
        nameField.accessibilityLabel = "Room name"
        nameField.borderStyle = .roundedRect
        nameField.font = .museScaled(ofSize: 17)
        nameField.adjustsFontForContentSizeCategory = true
        nameField.textAlignment = .center
        nameField.autocapitalizationType = .words
        nameField.addTarget(self, action: #selector(handleNameChanged), for: .editingChanged)
        nameField.translatesAutoresizingMaskIntoConstraints = false

        characterCountLabel.font = .museScaled(ofSize: 12, weight: .regular)
        characterCountLabel.adjustsFontForContentSizeCategory = true
        characterCountLabel.textColor = .tertiaryLabel
        characterCountLabel.textAlignment = .center

        validationLabel.font = .museScaled(ofSize: 13, weight: .regular)
        validationLabel.adjustsFontForContentSizeCategory = true
        validationLabel.textColor = .systemRed
        validationLabel.textAlignment = .center
        validationLabel.numberOfLines = 0

        zeroPhotoNoticeLabel.text = RoomCreationViewModel.zeroPhotoRoomsGateNotice
        zeroPhotoNoticeLabel.font = .museScaled(ofSize: 12, weight: .regular)
        zeroPhotoNoticeLabel.adjustsFontForContentSizeCategory = true
        zeroPhotoNoticeLabel.textColor = .tertiaryLabel
        zeroPhotoNoticeLabel.textAlignment = .center
        zeroPhotoNoticeLabel.numberOfLines = 0

        var continueConfiguration = UIButton.Configuration.filled()
        continueConfiguration.title = "Choose a Design"
        continueConfiguration.cornerStyle = .medium
        continueButton.configuration = continueConfiguration
        continueButton.addTarget(self, action: #selector(handleContinueTapped), for: .touchUpInside)
        continueButton.translatesAutoresizingMaskIntoConstraints = false

        let stack = UIStackView(arrangedSubviews: [
            promptLabel, nameField, characterCountLabel, validationLabel,
            zeroPhotoNoticeLabel, continueButton
        ])
        stack.axis = .vertical
        stack.spacing = 12
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -24),
            nameField.widthAnchor.constraint(greaterThanOrEqualToConstant: 260),
            continueButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 240)
        ])

        renderNameFeedback()
    }

    private func renderCharacterCount() {
        let over = viewModel.isOverLengthLimit
        characterCountLabel.text = over
            ? "\(viewModel.characterCountText) — too long"
            : viewModel.characterCountText
        characterCountLabel.textColor = over ? .systemRed : .tertiaryLabel
        characterCountLabel.accessibilityLabel = over
            ? "\(viewModel.characterCountText) characters — too long"
            : "\(viewModel.characterCountText) characters"
    }

    private func renderNameFeedback() {
        renderCharacterCount()
        validationLabel.text = viewModel.nameValidationMessage
        validationLabel.isHidden = viewModel.nameValidationMessage == nil
        announceValidationChange(viewModel.nameValidationMessage)
    }

    private func announceValidationChange(_ message: String?) {
        guard message != lastAnnouncedValidationMessage else { return }
        lastAnnouncedValidationMessage = message
        MuseAccessibility.announceFailure(message)
    }

    @objc private func handleNameChanged() {
        viewModel.updateName(nameField.text ?? "")
        renderNameFeedback()
    }

    @objc private func handleContinueTapped() {
        guard let name = viewModel.confirmedName() else { return }
        onNameConfirmed(name)
    }
}
