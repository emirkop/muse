import UIKit

final class PrivacySettingsViewController: UIViewController {
    typealias ConfirmationPresenting = (
        PrivacySettingsViewModel.ExposureConfirmation,
        @escaping (Bool) -> Void
    ) -> Void

    var confirmationPresenter: ConfirmationPresenting?

    private let viewModel: PrivacySettingsViewModel

    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let messageLabel = UILabel()
    private let scrollView = UIScrollView()
    private let contentStack = UIStackView()

    init(viewModel: PrivacySettingsViewModel) {
        self.viewModel = viewModel
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Privacy"
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
        messageLabel.font = .museScaled(ofSize: 15)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .secondaryLabel
        messageLabel.textAlignment = .center
        messageLabel.numberOfLines = 0

        contentStack.axis = .vertical
        contentStack.spacing = 20
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

    private func render(_ state: PrivacySettingsViewModel.State) {
        contentStack.arrangedSubviews.forEach { $0.removeFromSuperview() }

        switch state {
        case .loading:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Loading privacy settings"

        case .failed(let message):
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            messageLabel.text = message
            contentStack.addArrangedSubview(messageLabel)
            contentStack.addArrangedSubview(makeRetryButton())
            MuseAccessibility.announceFailure(message)

        case .loaded(let content):
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            if let notice = content.notice {
                contentStack.addArrangedSubview(makeNoticeLabel(notice))
            }
            contentStack.addArrangedSubview(makeMuseumSection(content))
            contentStack.addArrangedSubview(makeRoomsSection(content))
        }

        MuseAccessibility.announceLayoutChange()
    }

    // MARK: - Museum level

    private func makeMuseumSection(_ content: PrivacySettingsViewModel.Content) -> UIView {
        let control = makeSwitchRow(
            title: "Museum",
            stateLabel: content.museumStateLabel,
            isOn: content.museumIsPublic,
            isApplying: content.museumIsApplying,
            accessibilityLabel: "Museum privacy",
            action: { [weak self] isOn in self?.handleMuseumChange(to: isOn ? .public : .private) }
        )
        let explanation = makeDetailLabel(content.museumDescription)
        return makeSection(header: "Museum Privacy", rows: [control, explanation])
    }

    private func handleMuseumChange(to target: MusePrivacy) {
        guard let confirmation = viewModel.confirmation(forMuseumTarget: target) else {
            Task { await viewModel.setMuseumPrivacy(target) }
            return
        }
        requestConfirmation(confirmation) { [weak self] confirmed in
            guard let self else { return }
            guard confirmed else { return self.render(self.viewModel.state) }
            Task { await self.viewModel.setMuseumPrivacy(target) }
        }
    }

    // MARK: - Room level

    private func makeRoomsSection(_ content: PrivacySettingsViewModel.Content) -> UIView {
        guard !content.rooms.isEmpty else {
            return makeSection(
                header: "Room Privacy",
                rows: [makeDetailLabel("Your Museum has no Rooms yet.")]
            )
        }

        var rows: [UIView] = []
        for row in content.rooms {
            rows.append(makeSwitchRow(
                title: row.room.name,
                stateLabel: row.stateLabel,
                isOn: row.isPublic,
                isApplying: row.isApplying,
                accessibilityLabel: "\(row.room.name) privacy",
                action: { [weak self] isOn in
                    self?.handleRoomChange(row.room, to: isOn ? .public : .private)
                }
            ))
            rows.append(makeDetailLabel(row.visibilityDescription))
        }
        return makeSection(header: "Room Privacy", rows: rows)
    }

    private func handleRoomChange(_ room: Room, to target: MusePrivacy) {
        guard let confirmation = viewModel.confirmation(forRoom: room, target: target) else {
            Task { await viewModel.setPrivacy(target, forRoomWithID: room.id) }
            return
        }
        requestConfirmation(confirmation) { [weak self] confirmed in
            guard let self else { return }
            guard confirmed else { return self.render(self.viewModel.state) }
            Task { await self.viewModel.setPrivacy(target, forRoomWithID: room.id) }
        }
    }

    // MARK: - Views

    private func requestConfirmation(
        _ confirmation: PrivacySettingsViewModel.ExposureConfirmation,
        onAnswer: @escaping (Bool) -> Void
    ) {
        if let confirmationPresenter {
            confirmationPresenter(confirmation, onAnswer)
            return
        }
        let alert = UIAlertController(
            title: confirmation.title,
            message: confirmation.message,
            preferredStyle: .alert
        )
        alert.addAction(UIAlertAction(title: "Cancel", style: .cancel) { _ in onAnswer(false) })
        alert.addAction(UIAlertAction(title: confirmation.confirmTitle, style: .default) { _ in onAnswer(true) })
        present(alert, animated: true)
    }

    private func makeSection(header: String, rows: [UIView]) -> UIView {
        let headerLabel = UILabel()
        headerLabel.text = header.uppercased()
        headerLabel.font = .museScaled(ofSize: 13, weight: .semibold)
        headerLabel.adjustsFontForContentSizeCategory = true
        headerLabel.textColor = .secondaryLabel
        headerLabel.numberOfLines = 0
        headerLabel.accessibilityLabel = header
        headerLabel.museMarkAsHeader()

        let stack = UIStackView(arrangedSubviews: [headerLabel] + rows)
        stack.axis = .vertical
        stack.spacing = 10
        return stack
    }

    private func makeSwitchRow(
        title: String,
        stateLabel: String,
        isOn: Bool,
        isApplying: Bool,
        accessibilityLabel: String,
        action: @escaping (Bool) -> Void
    ) -> UIView {
        let titleLabel = UILabel()
        titleLabel.text = title
        titleLabel.font = .museScaled(ofSize: 17)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.numberOfLines = 0

        let state = UILabel()
        state.text = isApplying ? "Saving…" : stateLabel
        state.font = .museScaled(ofSize: 15, weight: .medium)
        state.adjustsFontForContentSizeCategory = true
        state.textColor = isApplying ? .tertiaryLabel : .secondaryLabel

        let toggle = UISwitch()
        toggle.isOn = isOn
        toggle.isEnabled = !isApplying
        toggle.accessibilityLabel = accessibilityLabel
        toggle.accessibilityValue = isApplying ? "Saving" : stateLabel
        toggle.addAction(UIAction { sender in
            guard let sender = sender.sender as? UISwitch else { return }
            action(sender.isOn)
        }, for: .valueChanged)

        let stack = UIStackView(arrangedSubviews: [titleLabel, state, toggle])
        stack.spacing = 10
        titleLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        MuseAccessibility.reflowForTextSize(stack, on: self)
        return stack
    }

    private func makeDetailLabel(_ text: String) -> UILabel {
        let label = UILabel()
        label.text = text
        label.font = .museScaled(ofSize: 13)
        label.adjustsFontForContentSizeCategory = true
        label.textColor = .secondaryLabel
        label.numberOfLines = 0
        return label
    }

    private func makeNoticeLabel(_ text: String) -> UILabel {
        let label = UILabel()
        label.text = text
        label.font = .museScaled(ofSize: 14, weight: .medium)
        label.adjustsFontForContentSizeCategory = true
        label.textColor = .systemRed
        label.numberOfLines = 0
        return label
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

extension PrivacySettingsViewController {
    var testSwitchStates: [String: Bool] {
        var states: [String: Bool] = [:]
        for toggle in contentStack.allSubviews.compactMap({ $0 as? UISwitch }) {
            if let label = toggle.accessibilityLabel { states[label] = toggle.isOn }
        }
        return states
    }

    var testVisibleText: [String] {
        contentStack.allSubviews.compactMap { ($0 as? UILabel)?.text }
    }

    func testFlipSwitch(labelled label: String, to isOn: Bool) {
        guard let toggle = contentStack.allSubviews
            .compactMap({ $0 as? UISwitch })
            .first(where: { $0.accessibilityLabel == label })
        else { return }
        toggle.isOn = isOn
        toggle.sendActions(for: .valueChanged)
    }
}

private extension UIView {
    var allSubviews: [UIView] {
        subviews + subviews.flatMap(\.allSubviews)
    }
}
