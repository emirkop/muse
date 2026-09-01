import Foundation

public enum NetworkFailureCopy {

    public enum Operation: Sendable {
        case read
        case mutation
    }

    public static func message(
        for error: Error,
        operation: Operation,
        otherwise fallback: String
    ) -> String {
        switch NetworkResilience.classify(error) {
        case .offline:
            switch operation {
            case .read:
                return "You're offline. Reconnect and this will load — there's no need to restart Muse."
            case .mutation:
                return "You're offline, so this change wasn't saved. It needs a connection — try again once you're back online."
            }
        case .unreachable:
            switch operation {
            case .read:
                return "Couldn't reach Muse. Check your connection and try again."
            case .mutation:
                return "Couldn't reach Muse. Check your connection, then check whether the change went through before trying again."
            }
        case .cancelled, .other:
            return fallback
        }
    }

    public static func mutationOutcome(
        for error: Error,
        certainlyUnchanged: String,
        possiblyApplied: String
    ) -> String {
        NetworkResilience.requestCertainlyNotDelivered(error)
            ? certainlyUnchanged
            : possiblyApplied
    }

    public static let outcomeUnknownTail =
        "It may or may not have been saved — reload to see the current state."
}
