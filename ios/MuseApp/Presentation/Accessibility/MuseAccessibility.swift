import UIKit

@MainActor
enum MuseAccessibility {

    // MARK: - Dynamic Type

    static var usesAccessibilityTextSize: Bool {
        UITraitCollection.current.preferredContentSizeCategory.isAccessibilityCategory
    }

    static func reflowForTextSize(
        _ stack: UIStackView,
        horizontalAlignment: UIStackView.Alignment = .center,
        verticalAlignment: UIStackView.Alignment = .leading,
        on owner: UIViewController
    ) {
        func apply() {
            let vertical = usesAccessibilityTextSize
            stack.axis = vertical ? .vertical : .horizontal
            stack.alignment = vertical ? verticalAlignment : horizontalAlignment
        }
        apply()
        owner.registerForTraitChanges([UITraitPreferredContentSizeCategory.self]) {
            (_: UITraitEnvironment, _: UITraitCollection) in
            apply()
        }
    }

    // MARK: - VoiceOver announcements

    static func announce(_ message: String?, priority: UIAccessibilityPriority = .default) {
        guard UIAccessibility.isVoiceOverRunning,
              let message, !message.isEmpty else { return }
        let announcement = NSAttributedString(
            string: message,
            attributes: [.accessibilitySpeechAnnouncementPriority: priority]
        )
        UIAccessibility.post(notification: .announcement, argument: announcement)
    }

    static func announceFailure(_ message: String?) {
        announce(message, priority: .high)
    }

    static func announceLayoutChange(focusing element: Any? = nil) {
        UIAccessibility.post(notification: .layoutChanged, argument: element)
    }

    static func announceScreenChange(focusing element: Any? = nil) {
        UIAccessibility.post(notification: .screenChanged, argument: element)
    }

    // MARK: - Minimum tap target

    static func ensureMinimumTapTarget(_ view: UIView, edge: CGFloat = 44) {
        view.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            view.heightAnchor.constraint(greaterThanOrEqualToConstant: edge),
            view.widthAnchor.constraint(greaterThanOrEqualToConstant: edge)
        ])
    }
}

extension UIFont {

    static func museScaled(ofSize size: CGFloat, weight: UIFont.Weight = .regular) -> UIFont {
        UIFontMetrics(forTextStyle: nearestTextStyle(forPointSize: size))
            .scaledFont(for: .systemFont(ofSize: size, weight: weight))
    }

    private static func nearestTextStyle(forPointSize size: CGFloat) -> UIFont.TextStyle {
        let table: [(CGFloat, UIFont.TextStyle)] = [
            (34, .largeTitle), (28, .title1), (22, .title2), (20, .title3),
            (17, .body), (16, .callout), (15, .subheadline),
            (13, .footnote), (12, .caption1), (11, .caption2)
        ]
        return table.min(by: { abs($0.0 - size) < abs($1.0 - size) })?.1 ?? .body
    }
}

extension UILabel {

    func museMarkAsHeader() {
        accessibilityTraits.insert(.header)
    }
}
