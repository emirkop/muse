import UIKit

public final class CaptionEditorViewController: UIViewController {
    public typealias Save = (String) async -> CaptionRules.CaptionSaveOutcome

    private let originalCaption: String
    private let save: Save
    private let onFinished: () -> Void

    private let card = UIView()
    private let field = UITextField()
    private let countLabel = UILabel()
    private let errorLabel = UILabel()
    private let saveButton = UIButton(type: .system)
    private let cancelButton = UIButton(type: .system)
    private let spinner = UIActivityIndicatorView(style: .medium)

    private var isSaving = false

    public init(caption: String, save: @escaping Save, onFinished: @escaping () -> Void) {
        self.originalCaption = CaptionRules.normalised(caption)
        self.save = save
        self.onFinished = onFinished
        super.init(nibName: nil, bundle: nil)
        modalPresentationStyle = .overFullScreen
        modalTransitionStyle = .crossDissolve
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    public override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .clear
        buildCard()
        render()
    }

    public override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        field.becomeFirstResponder()
    }

    // MARK: - Layout

    private func buildCard() {
        card.backgroundColor = .secondarySystemBackground
        card.layer.cornerRadius = 18
        card.layer.cornerCurve = .continuous
        card.translatesAutoresizingMaskIntoConstraints = false
        card.accessibilityIdentifier = "caption-editor-card"
        view.addSubview(card)

        let title = UILabel()
        title.text = originalCaption.isEmpty ? "Add a caption" : "Caption"
        title.museMarkAsHeader()
        title.font = .preferredFont(forTextStyle: .headline)
        title.adjustsFontForContentSizeCategory = true

        field.text = originalCaption
        field.placeholder = "Say something about this photograph"
        field.borderStyle = .roundedRect
        field.backgroundColor = .tertiarySystemBackground
        field.font = .preferredFont(forTextStyle: .body)
        field.adjustsFontForContentSizeCategory = true
        field.clearButtonMode = .whileEditing
        field.returnKeyType = .done
        field.enablesReturnKeyAutomatically = false
        field.autocorrectionType = .default
        field.accessibilityIdentifier = "caption-editor-field"
        field.accessibilityLabel = "Caption"
        field.addTarget(self, action: #selector(handleTextChanged), for: .editingChanged)
        field.addTarget(self, action: #selector(handleSave), for: .editingDidEndOnExit)

        countLabel.font = .preferredFont(forTextStyle: .caption2)
        countLabel.adjustsFontForContentSizeCategory = true
        countLabel.textColor = .secondaryLabel
        countLabel.accessibilityIdentifier = "caption-editor-count"

        errorLabel.font = .preferredFont(forTextStyle: .caption1)
        errorLabel.adjustsFontForContentSizeCategory = true
        errorLabel.textColor = .systemRed
        errorLabel.numberOfLines = 0
        errorLabel.isHidden = true
        errorLabel.accessibilityIdentifier = "caption-editor-error"

        cancelButton.setTitle("Cancel", for: .normal)
        cancelButton.addTarget(self, action: #selector(handleCancel), for: .touchUpInside)
        cancelButton.accessibilityIdentifier = "caption-editor-cancel"

        var saveConfig = UIButton.Configuration.filled()
        saveConfig.cornerStyle = .large
        saveButton.configuration = saveConfig
        saveButton.addTarget(self, action: #selector(handleSave), for: .touchUpInside)
        saveButton.accessibilityIdentifier = "caption-editor-save"

        spinner.hidesWhenStopped = true

        let buttons = UIStackView(arrangedSubviews: [cancelButton, spinner, UIView(), saveButton])
        buttons.axis = .horizontal
        buttons.spacing = 12
        buttons.alignment = .center

        let stack = UIStackView(arrangedSubviews: [title, field, countLabel, errorLabel, buttons])
        stack.axis = .vertical
        stack.spacing = 10
        stack.translatesAutoresizingMaskIntoConstraints = false
        card.addSubview(stack)

        NSLayoutConstraint.activate([
            card.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 12),
            card.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -12),
            card.bottomAnchor.constraint(equalTo: view.keyboardLayoutGuide.topAnchor, constant: -12),

            stack.topAnchor.constraint(equalTo: card.topAnchor, constant: 16),
            stack.leadingAnchor.constraint(equalTo: card.leadingAnchor, constant: 16),
            stack.trailingAnchor.constraint(equalTo: card.trailingAnchor, constant: -16),
            stack.bottomAnchor.constraint(equalTo: card.bottomAnchor, constant: -16)
        ])
    }

    // MARK: - Rendering

    private var currentText: String { field.text ?? "" }

    private func render() {
        let normalised = CaptionRules.normalised(currentText)
        let rejection = CaptionRules.rejection(for: currentText)

        countLabel.text = "\(CaptionRules.characterCount(currentText)) characters"
        countLabel.textColor = rejection == nil ? .secondaryLabel : .systemRed

        let title: String
        if normalised.isEmpty {
            title = originalCaption.isEmpty ? "Save" : "Remove Caption"
        } else {
            title = "Save"
        }
        saveButton.configuration?.title = title

        let unchanged = normalised == originalCaption
        saveButton.isEnabled = !isSaving && rejection == nil && !unchanged
        cancelButton.isEnabled = !isSaving
        field.isEnabled = !isSaving

        if let rejection {
            showError(CaptionRules.message(for: rejection))
        }
    }

    private func showError(_ message: String) {
        errorLabel.text = message
        errorLabel.isHidden = false
        MuseAccessibility.announceFailure(message)
    }

    private func clearError() {
        errorLabel.isHidden = true
        errorLabel.text = nil
    }

    // MARK: - Actions

    @objc private func handleTextChanged() {
        clearError()
        render()
    }

    @objc private func handleCancel() {
        guard !isSaving else { return }
        field.resignFirstResponder()
        dismiss(animated: true) { [onFinished] in onFinished() }
    }

    @objc private func handleSave() {
        guard !isSaving, saveButton.isEnabled else { return }
        let text = currentText
        isSaving = true
        clearError()
        spinner.startAnimating()
        render()

        Task { [weak self] in
            guard let self else { return }
            let outcome = await self.save(text)
            self.isSaving = false
            self.spinner.stopAnimating()

            switch outcome {
            case .saved:
                self.field.resignFirstResponder()
                self.dismiss(animated: true) { [onFinished = self.onFinished] in onFinished() }
            case .rejected(let message), .failed(let message):
                self.showError(message)
                self.render()
            }
        }
    }

    // MARK: - Test seam

    func testSetText(_ text: String) {
        field.text = text
        handleTextChanged()
    }

    var testSaveEnabled: Bool { saveButton.isEnabled }
    var testSaveTitle: String? { saveButton.configuration?.title }
    var testErrorMessage: String? { errorLabel.isHidden ? nil : errorLabel.text }
    var testCountText: String? { countLabel.text }
    var testFieldText: String? { field.text }

    func testTapSave() { handleSave() }
    func testTapCancel() { handleCancel() }
}
