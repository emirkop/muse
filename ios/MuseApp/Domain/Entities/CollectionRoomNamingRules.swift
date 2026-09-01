import Foundation

public enum CollectionRoomNamingRules {
    public static let interimMaximumLength = 60

    public static let interimEnforcesUniqueness = false

    public static let interimAppliesProfanityFilter = false

    public static let placeholderExample = "e.g. Watches"

    public enum Rejection: Equatable, Sendable {
        case empty
        case tooLong(limit: Int)
    }

    public static func rejection(for name: String) -> Rejection? {
        if name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return .empty
        }
        if name.count > interimMaximumLength {
            return .tooLong(limit: interimMaximumLength)
        }
        return nil
    }

    public static func message(for rejection: Rejection) -> String {
        switch rejection {
        case .empty:
            return "Give your Collection Room a name to continue."
        case .tooLong(let limit):
            return "Collection Room names are limited to \(limit) characters for now."
        }
    }
}
