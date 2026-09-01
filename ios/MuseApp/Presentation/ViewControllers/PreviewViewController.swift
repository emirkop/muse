import UIKit

final class PreviewViewController: UIViewController {
    // MARK: - Properties

    private let viewModel: PreviewViewModel
    private let onChoose: (String) -> Void
    private let onBack: () -> Void

    private let titleLabel = UILabel()
    private let statusLabel = UILabel()
    private let reassuranceLabel = UILabel()
    private let progressView = UIProgressView(progressViewStyle: .default)
    private let activityIndicator = UIActivityIndicatorView(style: .medium)
    private let previewContainer = UIView()
    private let chooseButton = UIButton(configuration: .filled())

    private let backButtonTitle: String
    private var lastAnnouncedStateIdentity: String?

    // MARK: - Initialization

    init(
        viewModel: PreviewViewModel,
        backButtonTitle: String,
        onChoose: @escaping (String) -> Void,
        onBack: @escaping () -> Void
    ) {
        self.backButtonTitle = backButtonTitle
        self.viewModel = viewModel
        self.onChoose = onChoose
        self.onBack = onBack
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        configureLayout()

        viewModel.onStateChange = { [weak self] state in
            self?.render(state)
        }
        Task { await viewModel.load() }
    }

    private func configureLayout() {
        titleLabel.text = viewModel.subject.displayName
        titleLabel.font = .museScaled(ofSize: 24, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textColor = .label
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        previewContainer.backgroundColor = .secondarySystemBackground
        previewContainer.layer.cornerRadius = 12
        previewContainer.translatesAutoresizingMaskIntoConstraints = false
        previewContainer.isAccessibilityElement = true
        previewContainer.accessibilityLabel = "Preview of \(viewModel.subject.displayName)"
        previewContainer.accessibilityTraits.insert(.image)

        statusLabel.font = .museScaled(ofSize: 14, weight: .regular)
        statusLabel.adjustsFontForContentSizeCategory = true
        statusLabel.textColor = .secondaryLabel
        statusLabel.textAlignment = .center
        statusLabel.numberOfLines = 0

        reassuranceLabel.text = viewModel.confirmationReassurance
        reassuranceLabel.font = .museScaled(ofSize: 13, weight: .medium)
        reassuranceLabel.adjustsFontForContentSizeCategory = true
        reassuranceLabel.textColor = .label
        reassuranceLabel.textAlignment = .center
        reassuranceLabel.numberOfLines = 0
        reassuranceLabel.isHidden = viewModel.confirmationReassurance == nil

        progressView.isHidden = true
        progressView.translatesAutoresizingMaskIntoConstraints = false
        progressView.isAccessibilityElement = true
        progressView.accessibilityLabel = "Preview download progress"
        progressView.accessibilityTraits.insert(.updatesFrequently)

        var chooseConfiguration = UIButton.Configuration.filled()
        chooseConfiguration.title = viewModel.primaryActionTitle
        chooseConfiguration.cornerStyle = .medium
        chooseButton.configuration = chooseConfiguration
        chooseButton.isEnabled = viewModel.isPrimaryActionEnabled
        chooseButton.addTarget(self, action: #selector(handleChooseTapped), for: .touchUpInside)
        chooseButton.translatesAutoresizingMaskIntoConstraints = false

        var backConfiguration = UIButton.Configuration.plain()
        backConfiguration.title = backButtonTitle
        let backButton = UIButton(configuration: backConfiguration)
        backButton.addTarget(self, action: #selector(handleBackTapped), for: .touchUpInside)

        let stack = UIStackView(arrangedSubviews: [
            titleLabel, previewContainer, activityIndicator, progressView,
            statusLabel, reassuranceLabel, chooseButton, backButton
        ])
        stack.axis = .vertical
        stack.spacing = 16
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -24),
            previewContainer.widthAnchor.constraint(equalToConstant: 280),
            previewContainer.heightAnchor.constraint(equalToConstant: 180),
            progressView.widthAnchor.constraint(equalToConstant: 240),
            chooseButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 240)
        ])
    }

    // MARK: - Rendering

    private func render(_ state: PreviewViewModel.State) {
        statusLabel.text = viewModel.statusMessage

        switch state {
        case .checkingAssets:
            activityIndicator.startAnimating()
            activityIndicator.isAccessibilityElement = true
            activityIndicator.accessibilityLabel = "Checking whether this design is available"
            progressView.isHidden = true

        case .assetsUnavailable, .assetsUnreachable:
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            progressView.isHidden = true

        case .downloading(let fraction):
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            progressView.isHidden = false
            progressView.progress = Float(fraction)
            progressView.accessibilityValue = "\(Int(fraction * 100)) percent"

        case .geometryReady, .ready:
            activityIndicator.stopAnimating()
            activityIndicator.isAccessibilityElement = false
            progressView.isHidden = true
            presentImmersiveSurfaceIfNeeded()
        }

        chooseButton.isEnabled = viewModel.isPrimaryActionEnabled
        chooseButton.accessibilityHint = viewModel.isPrimaryActionEnabled ? nil : viewModel.statusMessage

        announceStateIfChanged(state)
    }

    private func announceStateIfChanged(_ state: PreviewViewModel.State) {
        let identity = String(String(describing: state).prefix { $0 != "(" })
        guard identity != lastAnnouncedStateIdentity else { return }
        lastAnnouncedStateIdentity = identity
        MuseAccessibility.announce(viewModel.statusMessage)
    }

    private func presentImmersiveSurfaceIfNeeded() {
        guard viewModel.shouldPresentImmersiveSurface, children.isEmpty else { return }

        let runtime: MuseumRuntimeInterface = RealityKitMuseumRuntime()
        let runtimeViewController = runtime.makeRuntimeViewController()

        addChild(runtimeViewController)
        runtimeViewController.view.translatesAutoresizingMaskIntoConstraints = false
        previewContainer.addSubview(runtimeViewController.view)
        NSLayoutConstraint.activate([
            runtimeViewController.view.topAnchor.constraint(equalTo: previewContainer.topAnchor),
            runtimeViewController.view.bottomAnchor.constraint(equalTo: previewContainer.bottomAnchor),
            runtimeViewController.view.leadingAnchor.constraint(equalTo: previewContainer.leadingAnchor),
            runtimeViewController.view.trailingAnchor.constraint(equalTo: previewContainer.trailingAnchor)
        ])
        runtimeViewController.didMove(toParent: self)
    }

    // MARK: - Actions

    @objc private func handleChooseTapped() {
        onChoose(viewModel.subject.id)
    }

    @objc private func handleBackTapped() {
        onBack()
    }
}
