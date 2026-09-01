import UIKit

final class MuseumCreationFramingViewController: UIViewController {
    private let onContinue: () -> Void

    init(onContinue: @escaping () -> Void) {
        self.onContinue = onContinue
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "Create Your Museum"
        view.backgroundColor = .systemBackground

        let headlineLabel = UILabel()
        headlineLabel.text = "Choose the style of your museum"
        headlineLabel.font = .museScaled(ofSize: 24, weight: .semibold)
        headlineLabel.adjustsFontForContentSizeCategory = true
        headlineLabel.textColor = .label
        headlineLabel.textAlignment = .center
        headlineLabel.numberOfLines = 0
        headlineLabel.museMarkAsHeader()

        let subtitleLabel = UILabel()
        subtitleLabel.text = "This sets the architecture, materials and lighting of your space — not just its colour. You can change it later without losing anything."
        subtitleLabel.font = .museScaled(ofSize: 15, weight: .regular)
        subtitleLabel.adjustsFontForContentSizeCategory = true
        subtitleLabel.textColor = .secondaryLabel
        subtitleLabel.textAlignment = .center
        subtitleLabel.numberOfLines = 0

        var configuration = UIButton.Configuration.filled()
        configuration.title = "Browse Styles"
        configuration.cornerStyle = .medium
        let continueButton = UIButton(configuration: configuration)
        continueButton.addTarget(self, action: #selector(handleContinueTapped), for: .touchUpInside)
        continueButton.translatesAutoresizingMaskIntoConstraints = false

        let stack = UIStackView(arrangedSubviews: [headlineLabel, subtitleLabel, continueButton])
        stack.axis = .vertical
        stack.spacing = 24
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -32),
            continueButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 240)
        ])
    }

    @objc private func handleContinueTapped() {
        onContinue()
    }
}
