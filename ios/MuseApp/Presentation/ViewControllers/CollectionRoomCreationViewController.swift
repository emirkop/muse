import UIKit

final class CollectionRoomCreationViewController: UIViewController {
    private let viewModel: CollectionRoomCreationViewModel
    private let onCreated: (CollectionRoom) -> Void

    private let scrollView = UIScrollView()
    private let contentStack = UIStackView()

    private let categoryPromptLabel = UILabel()
    private let categoryStack = UIStackView()
    private let categoryStatusLabel = UILabel()
    private let retryCategoriesButton = UIButton(configuration: .bordered())
    private let loadingIndicator = UIActivityIndicatorView(style: .medium)

    private let namePromptLabel = UILabel()
    private let nameField = UITextField()
    private let characterCountLabel = UILabel()
    private let validationLabel = UILabel()
    private let creationErrorLabel = UILabel()
    private let createButton = UIButton(configuration: .filled())

    private var categoryButtons: [String: UIButton] = [:]
    private var lastAnnouncedProblem: String?

    init(viewModel: CollectionRoomCreationViewModel, onCreated: @escaping (CollectionRoom) -> Void) {
        self.viewModel = viewModel
        self.onCreated = onCreated
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "New Collection Room"
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        render(viewModel.state)

        Task { await viewModel.loadCategories() }
    }

    // MARK: - Layout

    private func configureLayout() {
        categoryPromptLabel.text = "What do you collect?"
        categoryPromptLabel.font = .museScaled(ofSize: 22, weight: .semibold)
        categoryPromptLabel.adjustsFontForContentSizeCategory = true
        categoryPromptLabel.textColor = .label
        categoryPromptLabel.numberOfLines = 0
        categoryPromptLabel.museMarkAsHeader()

        categoryStack.axis = .vertical
        categoryStack.spacing = 10

        categoryStatusLabel.font = .museScaled(ofSize: 14)
        categoryStatusLabel.adjustsFontForContentSizeCategory = true
        categoryStatusLabel.textColor = .secondaryLabel
        categoryStatusLabel.numberOfLines = 0
        categoryStatusLabel.textAlignment = .center

        retryCategoriesButton.setTitle("Try Again", for: .normal)
        retryCategoriesButton.addTarget(self, action: #selector(handleRetryTapped), for: .touchUpInside)

        loadingIndicator.hidesWhenStopped = true
        loadingIndicator.isAccessibilityElement = true
        loadingIndicator.accessibilityLabel = "Loading categories"

        namePromptLabel.text = "Name this Collection Room"
        namePromptLabel.font = .museScaled(ofSize: 17, weight: .semibold)
        namePromptLabel.adjustsFontForContentSizeCategory = true
        namePromptLabel.textColor = .label
        namePromptLabel.numberOfLines = 0
        namePromptLabel.museMarkAsHeader()

        nameField.placeholder = CollectionRoomNamingRules.placeholderExample
        nameField.accessibilityLabel = "Collection Room name"
        nameField.borderStyle = .roundedRect
        nameField.font = .museScaled(ofSize: 17)
        nameField.adjustsFontForContentSizeCategory = true
        nameField.autocapitalizationType = .words
        nameField.addTarget(self, action: #selector(handleNameChanged), for: .editingChanged)

        characterCountLabel.font = .museScaled(ofSize: 12)
        characterCountLabel.adjustsFontForContentSizeCategory = true
        characterCountLabel.textColor = .tertiaryLabel

        validationLabel.font = .museScaled(ofSize: 13)
        validationLabel.adjustsFontForContentSizeCategory = true
        validationLabel.textColor = .systemRed
        validationLabel.numberOfLines = 0

        creationErrorLabel.font = .museScaled(ofSize: 13)
        creationErrorLabel.adjustsFontForContentSizeCategory = true
        creationErrorLabel.textColor = .systemRed
        creationErrorLabel.numberOfLines = 0

        createButton.setTitle("Create Collection Room", for: .normal)
        createButton.addTarget(self, action: #selector(handleCreateTapped), for: .touchUpInside)

        contentStack.axis = .vertical
        contentStack.spacing = 14
        contentStack.translatesAutoresizingMaskIntoConstraints = false
        [
            categoryPromptLabel, loadingIndicator, categoryStatusLabel, retryCategoriesButton,
            categoryStack, namePromptLabel, nameField, characterCountLabel,
            validationLabel, creationErrorLabel, createButton
        ].forEach(contentStack.addArrangedSubview)

        scrollView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.addSubview(contentStack)
        view.addSubview(scrollView)

        NSLayoutConstraint.activate([
            scrollView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            scrollView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            scrollView.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor),

            contentStack.topAnchor.constraint(equalTo: scrollView.topAnchor, constant: 24),
            contentStack.leadingAnchor.constraint(equalTo: scrollView.leadingAnchor, constant: 24),
            contentStack.trailingAnchor.constraint(equalTo: scrollView.trailingAnchor, constant: -24),
            contentStack.bottomAnchor.constraint(equalTo: scrollView.bottomAnchor, constant: -24),
            contentStack.widthAnchor.constraint(equalTo: scrollView.widthAnchor, constant: -48)
        ])
    }

    // MARK: - Rendering

    private func render(_ state: CollectionRoomCreationViewModel.State) {
        switch state {
        case .loadingCategories:
            loadingIndicator.startAnimating()
            categoryStatusLabel.text = "Loading categories…"
            categoryStatusLabel.isHidden = false
            retryCategoriesButton.isHidden = true
            setNameSectionHidden(true)

        case .ready(let categories, let selectedCategoryID):
            loadingIndicator.stopAnimating()
            categoryStatusLabel.isHidden = true
            retryCategoriesButton.isHidden = true
            renderCategoryCards(categories, selected: selectedCategoryID)
            setNameSectionHidden(false)

        case .noCategoriesAvailable:
            loadingIndicator.stopAnimating()
            categoryStatusLabel.text = CollectionRoomCreationViewModel.noCategoriesMessage
            categoryStatusLabel.isHidden = false
            retryCategoriesButton.isHidden = true
            renderCategoryCards([], selected: nil)
            setNameSectionHidden(true)

        case .categoriesFailed(let message):
            loadingIndicator.stopAnimating()
            categoryStatusLabel.text = message
            categoryStatusLabel.isHidden = false
            retryCategoriesButton.isHidden = false
            renderCategoryCards([], selected: nil)
            setNameSectionHidden(true)
        }

        renderNameFeedback()
    }

    private func setNameSectionHidden(_ hidden: Bool) {
        [namePromptLabel, nameField, characterCountLabel, createButton].forEach { $0.isHidden = hidden }
    }

    private func renderCategoryCards(_ categories: [CollectionCategory], selected: String?) {
        if Set(categoryButtons.keys) != Set(categories.map(\.id)) {
            categoryStack.arrangedSubviews.forEach { $0.removeFromSuperview() }
            categoryButtons = [:]
            for category in categories {
                let button = makeCategoryCard(for: category)
                categoryButtons[category.id] = button
                categoryStack.addArrangedSubview(button)
            }
        }
        for (id, button) in categoryButtons {
            let isSelected = id == selected
            button.configuration?.background.strokeColor = isSelected ? .tintColor : .separator
            button.configuration?.background.strokeWidth = isSelected ? 2 : 1
            button.accessibilityTraits = isSelected ? [.button, .selected] : [.button]
        }
    }

    private func makeCategoryCard(for category: CollectionCategory) -> UIButton {
        var configuration = UIButton.Configuration.plain()
        configuration.title = category.displayName
        configuration.image = UIImage(systemName: "square.grid.2x2")
        configuration.imagePadding = 12
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 16, leading: 16, bottom: 16, trailing: 16)
        configuration.background.cornerRadius = 12
        configuration.background.strokeColor = .separator
        configuration.background.strokeWidth = 1

        let button = UIButton(configuration: configuration)
        button.contentHorizontalAlignment = .leading
        button.accessibilityLabel = category.displayName
        button.addAction(UIAction { [weak self] _ in
            self?.viewModel.selectCategory(id: category.id)
        }, for: .touchUpInside)
        return button
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

        creationErrorLabel.text = viewModel.creationErrorMessage
        creationErrorLabel.isHidden = viewModel.creationErrorMessage == nil

        createButton.isEnabled = viewModel.canCreate
        createButton.configuration?.showsActivityIndicator = viewModel.isCreating

        announceIfChanged(viewModel.nameValidationMessage ?? viewModel.creationErrorMessage)
    }

    private func announceIfChanged(_ message: String?) {
        guard message != lastAnnouncedProblem else { return }
        lastAnnouncedProblem = message
        MuseAccessibility.announceFailure(message)
    }

    // MARK: - Actions

    @objc private func handleNameChanged() {
        viewModel.updateName(nameField.text ?? "")
        renderNameFeedback()
    }

    @objc private func handleRetryTapped() {
        Task { await viewModel.loadCategories() }
    }

    @objc private func handleCreateTapped() {
        nameField.resignFirstResponder()
        Task {
            if let room = await viewModel.createCollectionRoom() {
                onCreated(room)
            }
        }
    }
}
