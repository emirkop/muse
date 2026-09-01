import UIKit

final class RoomEntryViewController: UIViewController {
    // MARK: - Properties

    private let viewModel: RoomEntryViewModel
    private let onEnter: (RoomRuntimeContent) -> Void
    private let onCancel: (() -> Void)?

    private let titleLabel = UILabel()
    private let summaryLabel = UILabel()
    private let messageLabel = UILabel()
    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let progressView = UIProgressView(progressViewStyle: .default)
    private let retryButton = UIButton(configuration: .filled())
    private let cancelButton = UIButton(configuration: .plain())

    private var loadTask: Task<Void, Never>?
    private var lastAnnouncedStateIdentity: String?

    // MARK: - Initialization

    init(viewModel: RoomEntryViewModel, onCancel: (() -> Void)? = nil, onEnter: @escaping (RoomRuntimeContent) -> Void) {
        self.viewModel = viewModel
        self.onEnter = onEnter
        self.onCancel = onCancel
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        title = viewModel.room.name
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        startLoad()
    }

    override func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        if isMovingFromParent || isBeingDismissed {
            loadTask?.cancel()
        }
    }

    private func startLoad() {
        loadTask?.cancel()
        loadTask = Task { [viewModel] in await viewModel.load() }
    }

    private func configureLayout() {
        titleLabel.font = .museScaled(ofSize: 22, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        summaryLabel.font = .museScaled(ofSize: 15, weight: .medium)
        summaryLabel.adjustsFontForContentSizeCategory = true
        summaryLabel.textColor = .secondaryLabel
        summaryLabel.textAlignment = .center
        summaryLabel.numberOfLines = 0
        summaryLabel.text = viewModel.contentSummary

        messageLabel.font = .museScaled(ofSize: 15)
        messageLabel.adjustsFontForContentSizeCategory = true
        messageLabel.textColor = .secondaryLabel
        messageLabel.textAlignment = .center
        messageLabel.numberOfLines = 0

        progressView.translatesAutoresizingMaskIntoConstraints = false
        progressView.isAccessibilityElement = true
        progressView.accessibilityLabel = "Design download progress"
        progressView.accessibilityTraits.insert(.updatesFrequently)

        var retry = UIButton.Configuration.filled()
        retry.title = "Try Again"
        retry.cornerStyle = .medium
        retryButton.configuration = retry
        retryButton.addTarget(self, action: #selector(handleRetry), for: .touchUpInside)
        retryButton.translatesAutoresizingMaskIntoConstraints = false

        var cancel = UIButton.Configuration.plain()
        cancel.title = "Cancel"
        cancelButton.configuration = cancel
        cancelButton.addTarget(self, action: #selector(handleCancel), for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [
            titleLabel, summaryLabel, activityIndicator, progressView, messageLabel, retryButton, cancelButton
        ])
        stack.axis = .vertical
        stack.spacing = 14
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -32),
            retryButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 200),
            progressView.widthAnchor.constraint(equalToConstant: 240)
        ])
        render(viewModel.state)
    }

    // MARK: - Rendering

    private func render(_ state: RoomEntryViewModel.State) {
        let copy = viewModel.loadingCopy(for: state)
        titleLabel.text = copy.title
        messageLabel.text = copy.detail

        retryButton.isHidden = true
        cancelButton.isHidden = true
        progressView.isHidden = true
        activityIndicator.stopAnimating()

        switch state {
        case .checking:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = copy.title
            cancelButton.isHidden = !viewModel.showsExtendedWaitCopy || onCancel == nil

        case .downloading(let fraction), .geometryReady(let fraction):
            progressView.isHidden = false
            progressView.setProgress(Float(fraction), animated: true)
            progressView.accessibilityValue = "\(Int(fraction * 100)) percent"
            cancelButton.isHidden = onCancel == nil

        case .designUnavailable(_, let reason):
            retryButton.isHidden = !viewModel.designUnavailableCopy(reason).canRetry

        case .placementUnresolvable:
            retryButton.isHidden = false

        case .ready:
            if let content = viewModel.content {
                onEnter(content)
            }
        }

        announceStateIfChanged(state, copy: copy)
    }

    private func announceStateIfChanged(
        _ state: RoomEntryViewModel.State,
        copy: (title: String, detail: String?)
    ) {
        let identity = String(String(describing: state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity

        let message = [copy.title, copy.detail].compactMap { $0 }.joined(separator: ". ")
        switch state {
        case .designUnavailable, .placementUnresolvable:
            MuseAccessibility.announceFailure(message)
        default:
            MuseAccessibility.announce(message)
        }
    }

    // MARK: - Actions

    @objc private func handleRetry() {
        startLoad()
    }

    @objc private func handleCancel() {
        loadTask?.cancel()
        onCancel?()
    }
}
