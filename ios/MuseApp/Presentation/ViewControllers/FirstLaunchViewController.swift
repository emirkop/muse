import UIKit

final class FirstLaunchViewController: UIViewController {
    // MARK: - Properties

    private let onContinue: () -> Void

    private let taglines = [
        "Your photos become a real place.",
        "Walk through a museum you build.",
        "Your collection, displayed in 3D."
    ]
    private var currentIndex = 0
    private var advanceTimer: Timer?

    private let taglineLabel = UILabel()
    private let skipButton = UIButton(configuration: .plain())
    private var cinematicContainer: UIView!
    private var callToActionContainer: UIView!

    // MARK: - Initialization

    init(onContinue: @escaping () -> Void) {
        self.onContinue = onContinue
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
        renderTagline()
        scheduleAutoAdvance()
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        advanceTimer?.invalidate()
    }

    private func configureLayout() {
        let titleLabel = UILabel()
        titleLabel.text = "Muse"
        titleLabel.font = .museScaled(ofSize: 28, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textColor = .label
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        taglineLabel.font = .museScaled(ofSize: 20, weight: .medium)
        taglineLabel.adjustsFontForContentSizeCategory = true
        taglineLabel.textColor = .label
        taglineLabel.textAlignment = .center
        taglineLabel.numberOfLines = 0
        taglineLabel.isUserInteractionEnabled = true
        taglineLabel.addGestureRecognizer(UITapGestureRecognizer(target: self, action: #selector(handleTaglineTapped)))
        taglineLabel.isAccessibilityElement = true
        taglineLabel.accessibilityTraits.insert(.button)
        taglineLabel.accessibilityHint = "Continues to the next screen"

        var skipConfiguration = UIButton.Configuration.plain()
        skipConfiguration.title = "Skip"
        skipButton.configuration = skipConfiguration
        skipButton.addTarget(self, action: #selector(handleSkipTapped), for: .touchUpInside)

        let cinematicStack = UIStackView(arrangedSubviews: [taglineLabel, skipButton])
        cinematicStack.axis = .vertical
        cinematicStack.spacing = 20
        cinematicStack.alignment = .center
        cinematicContainer = cinematicStack

        var getStartedConfiguration = UIButton.Configuration.filled()
        getStartedConfiguration.title = "Get Started"
        let getStartedButton = UIButton(configuration: getStartedConfiguration)
        getStartedButton.addTarget(self, action: #selector(handleContinueTapped), for: .touchUpInside)
        getStartedButton.translatesAutoresizingMaskIntoConstraints = false

        var logInConfiguration = UIButton.Configuration.plain()
        logInConfiguration.title = "Log In"
        let logInButton = UIButton(configuration: logInConfiguration)
        logInButton.addTarget(self, action: #selector(handleContinueTapped), for: .touchUpInside)

        let ctaStack = UIStackView(arrangedSubviews: [getStartedButton, logInButton])
        ctaStack.axis = .vertical
        ctaStack.spacing = 12
        ctaStack.alignment = .center
        ctaStack.isHidden = true
        callToActionContainer = ctaStack

        let rootStack = UIStackView(arrangedSubviews: [titleLabel, cinematicStack, ctaStack])
        rootStack.axis = .vertical
        rootStack.spacing = 32
        rootStack.alignment = .center
        rootStack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(rootStack)

        NSLayoutConstraint.activate([
            rootStack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            rootStack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            rootStack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 32),
            rootStack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -32),
            getStartedButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 220)
        ])
    }

    private func renderTagline() {
        taglineLabel.text = taglines[currentIndex]
        MuseAccessibility.announce(taglines[currentIndex])
    }

    private var suppressesAutoAdvance: Bool {
        UIAccessibility.isVoiceOverRunning
            || UIAccessibility.isSwitchControlRunning
            || UIAccessibility.isAssistiveTouchRunning
    }

    private func scheduleAutoAdvance() {
        advanceTimer?.invalidate()
        guard !suppressesAutoAdvance else { return }
        advanceTimer = Timer.scheduledTimer(
            timeInterval: 2.5,
            target: self,
            selector: #selector(handleAutoAdvance),
            userInfo: nil,
            repeats: false
        )
    }

    // MARK: - Actions

    @objc private func handleAutoAdvance() {
        advance()
    }

    @objc private func handleTaglineTapped() {
        advance()
    }

    @objc private func handleSkipTapped() {
        showCallToAction()
    }

    private func advance() {
        currentIndex += 1
        if currentIndex >= taglines.count {
            showCallToAction()
        } else {
            renderTagline()
            scheduleAutoAdvance()
        }
    }

    private func showCallToAction() {
        advanceTimer?.invalidate()
        cinematicContainer.isHidden = true
        callToActionContainer.isHidden = false
        MuseAccessibility.announceScreenChange(focusing: callToActionContainer)
    }

    @objc private func handleContinueTapped() {
        onContinue()
    }
}
