import Foundation

public enum LaunchSessionRouting {
    public enum Verdict: Equatable, Sendable {
        case serverUnreachable
        case sessionInvalid
    }

    public static func verdict(for error: Error) -> Verdict {
        if let apiError = error as? IdentityAPIClientError, apiError.isConnectivityFailure {
            return .serverUnreachable
        }
        return NetworkResilience.permitsCachedRead(NetworkResilience.classify(error))
            ? .serverUnreachable
            : .sessionInvalid
    }
}
