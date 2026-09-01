import UIKit

final class AccountCreationViewController: UIViewController {
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
        view.backgroundColor = .systemBackground

        let titleLabel = UILabel()
        titleLabel.text = "Welcome to Muse"
        titleLabel.font = .museScaled(ofSize: 28, weight: .semibold)
        titleLabel.adjustsFontForContentSizeCategory = true
        titleLabel.textColor = .label
        titleLabel.textAlignment = .center
        titleLabel.numberOfLines = 0
        titleLabel.museMarkAsHeader()

        let consentLabel = UILabel()
        consentLabel.text = "By continuing, you agree to Muse's Terms of Service and Privacy Policy."
        consentLabel.font = .museScaled(ofSize: 14, weight: .regular)
        consentLabel.adjustsFontForContentSizeCategory = true
        consentLabel.textColor = .secondaryLabel
        consentLabel.textAlignment = .center
        consentLabel.numberOfLines = 0

        var buttonConfiguration = UIButton.Configuration.filled()
        buttonConfiguration.title = "Agree & Continue"
        buttonConfiguration.cornerStyle = .medium
        let continueButton = UIButton(configuration: buttonConfiguration)
        continueButton.addTarget(self, action: #selector(handleContinueTapped), for: .touchUpInside)
        continueButton.translatesAutoresizingMaskIntoConstraints = false

        let stack = UIStackView(arrangedSubviews: [titleLabel, consentLabel, continueButton])
        stack.axis = .vertical
        stack.spacing = 20
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)

        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            stack.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -32),
            continueButton.widthAnchor.constraint(greaterThanOrEqualToConstant: 260)
        ])
    }

    @objc private func handleContinueTapped() {
        onContinue()
    }
}
