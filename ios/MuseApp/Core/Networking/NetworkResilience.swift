import Foundation

public enum NetworkResilience {

    public enum Classification: Equatable, Sendable {
        case offline
        case unreachable
        case cancelled
        case other
    }

    public static func classify(_ error: Error) -> Classification {
        if error is CancellationError { return .cancelled }
        guard let urlError = error as? URLError else {
            return (error as NSError).code == NSURLErrorCancelled ? .cancelled : .other
        }
        switch urlError.code {
        case .notConnectedToInternet, .networkConnectionLost,
             .dataNotAllowed, .internationalRoamingOff, .callIsActive:
            return .offline
        case .cancelled:
            return .cancelled
        case .timedOut, .cannotFindHost, .cannotConnectToHost, .dnsLookupFailed,
             .secureConnectionFailed, .serverCertificateUntrusted, .badServerResponse,
             .resourceUnavailable, .httpTooManyRedirects:
            return .unreachable
        default:
            return .unreachable
        }
    }

    public static func permitsCachedRead(_ classification: Classification) -> Bool {
        switch classification {
        case .offline, .unreachable: return true
        case .cancelled, .other: return false
        }
    }

    // MARK: - Sessions

    public enum Timeouts {
        public static let apiRequest: TimeInterval = 15
        public static let apiResource: TimeInterval = 30
        public static let uploadResource: TimeInterval = 300
    }

    public static func apiSession() -> URLSession {
        let configuration = URLSessionConfiguration.default
        configuration.timeoutIntervalForRequest = Timeouts.apiRequest
        configuration.timeoutIntervalForResource = Timeouts.apiResource
        configuration.waitsForConnectivity = false
        return URLSession(configuration: configuration)
    }

    public static func uploadSession() -> URLSession {
        let configuration = URLSessionConfiguration.default
        configuration.timeoutIntervalForRequest = Timeouts.apiRequest
        configuration.timeoutIntervalForResource = Timeouts.uploadResource
        configuration.waitsForConnectivity = false
        return URLSession(configuration: configuration)
    }

    // MARK: - Bounded retry

    public struct RetryPolicy: Sendable {
        public let attempts: Int
        public let initialDelay: TimeInterval
        public let multiplier: Double
        public let jitter: Double

        public static let read = RetryPolicy(attempts: 3, initialDelay: 0.3, multiplier: 2.5, jitter: 0.25)
        public static let none = RetryPolicy(attempts: 1, initialDelay: 0, multiplier: 1, jitter: 0)

        public init(attempts: Int, initialDelay: TimeInterval, multiplier: Double, jitter: Double) {
            self.attempts = max(1, attempts)
            self.initialDelay = initialDelay
            self.multiplier = multiplier
            self.jitter = jitter
        }

        func delay(forAttempt attempt: Int) -> TimeInterval {
            let base = initialDelay * pow(multiplier, Double(attempt - 1))
            guard jitter > 0 else { return base }
            return base * (1 + Double.random(in: -jitter...jitter))
        }
    }

    public static func withRetry<T: Sendable>(
        _ policy: RetryPolicy,
        shouldRetry: @Sendable (Error) -> Bool = { permitsCachedRead(classify($0)) },
        operation: @Sendable () async throws -> T
    ) async throws -> T {
        var lastError: Error?
        for attempt in 1...policy.attempts {
            do {
                return try await operation()
            } catch {
                lastError = error
                if classify(error) == .cancelled || Task.isCancelled { throw error }
                guard attempt < policy.attempts, shouldRetry(error) else { throw error }
                let seconds = policy.delay(forAttempt: attempt)
                if seconds > 0 {
                    try? await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
                }
                if Task.isCancelled { throw error }
            }
        }
        throw lastError ?? URLError(.unknown)
    }
}

// MARK: - The one retry decision

extension NetworkResilience {

    public static func requestCertainlyNotDelivered(_ error: Error) -> Bool {
        guard let urlError = error as? URLError else {
            return true
        }
        switch urlError.code {
        case .notConnectedToInternet, .dataNotAllowed,
             .internationalRoamingOff, .callIsActive:
            return true
        default:
            return false
        }
    }

    public static func isRetryableForRead(_ error: Error) -> Bool {
        switch classify(error) {
        case .unreachable:
            return true
        case .offline:
            return (error as? URLError)?.code == .networkConnectionLost
        case .cancelled, .other:
            return false
        }
    }
}

extension URLSession {

    func resilientData(for request: URLRequest) async throws -> (Data, URLResponse) {
        let method = (request.httpMethod ?? "GET").uppercased()
        let isSafeRead = method == "GET" || method == "HEAD"
        let policy: NetworkResilience.RetryPolicy = isSafeRead ? .read : .none
        return try await NetworkResilience.withRetry(
            policy,
            shouldRetry: { NetworkResilience.isRetryableForRead($0) },
            operation: { try await self.data(for: request) }
        )
    }
}
