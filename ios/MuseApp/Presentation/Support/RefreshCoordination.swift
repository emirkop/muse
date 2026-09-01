import Foundation

@MainActor
public final class RefreshCoordination {
    private var generation = 0

    public init() {}

    public func begin() -> Int {
        generation += 1
        return generation
    }

    public func isCurrent(_ token: Int) -> Bool {
        token == generation
    }
}

public enum RefreshFailureNotice {
    public static func message(for error: Error) -> String {
        switch NetworkResilience.classify(error) {
        case .offline:
            return "You're offline — showing what was last loaded."
        case .unreachable:
            return "Couldn't reach Muse — showing what was last loaded."
        case .cancelled, .other:
            return "Couldn't refresh — showing what was last loaded."
        }
    }
}
