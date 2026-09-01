import Foundation

public enum RoomNamingRules {
    public static let interimMaximumLength = 60

    public static let interimEnforcesUniqueness = false

    public static let interimAppliesProfanityFilter = false

    public static let placeholderExample = "e.g. Trabzon"

    public enum Rejection: Equatable {
        case empty
        case tooLong(limit: Int)
    }

    public static func rejection(for name: String) -> Rejection? {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
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
            return "Give your Room a name to continue."
        case .tooLong(let limit):
            return "Room names are limited to \(limit) characters for now."
        }
    }
}
