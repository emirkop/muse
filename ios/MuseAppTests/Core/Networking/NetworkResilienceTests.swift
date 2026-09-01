import XCTest
@testable import MuseApp

final class NetworkResilienceTests: XCTestCase {

    // MARK: - Case 8 — auth/share/entitlement failures are never offline

    func test_serverRefusals_areNeverConnectivityFailures() {
        let refusals: [Error] = [
            IdentityAPIClientError.server(statusCode: 401, message: "unauthorized"),
            IdentityAPIClientError.server(statusCode: 403, message: "forbidden"),
            IdentityAPIClientError.server(statusCode: 404, message: "not found"),
            IdentityAPIClientError.server(statusCode: 429, message: "too many"),
            IdentityAPIClientError.server(statusCode: 503, message: "unavailable"),
            PhotoAPIError(statusCode: 403, message: "forbidden", code: nil, assetID: nil),
            PhotoAPIError(statusCode: 404, message: "not found", code: nil, assetID: nil),
            CollectionAPIError(statusCode: 402, code: "item_capacity_reached", message: "upgrade"),
        ]
        for refusal in refusals {
            XCTAssertEqual(NetworkResilience.classify(refusal), .other,
                           "\(refusal) must not classify as a connectivity failure")
            XCTAssertFalse(NetworkResilience.permitsCachedRead(NetworkResilience.classify(refusal)),
                           "\(refusal) must never license a cached read")
            XCTAssertFalse(NetworkResilience.isRetryableForRead(refusal),
                           "\(refusal) is the server's answer, not a transport failure")
        }
    }

    func test_serverRefusalCopy_isNeverTheOfflineMessage() {
        let fallback = "Couldn't apply that design. Please try again."
        for operation in [NetworkFailureCopy.Operation.read, .mutation] {
            let message = NetworkFailureCopy.message(
                for: IdentityAPIClientError.server(statusCode: 403, message: nil),
                operation: operation,
                otherwise: fallback
            )
            XCTAssertEqual(message, fallback)
            XCTAssertFalse(message.lowercased().contains("offline"))
        }
    }

    func test_offlineCodes_classifyAsOffline() {
        for code in [URLError.Code.notConnectedToInternet, .networkConnectionLost,
                     .dataNotAllowed, .internationalRoamingOff, .callIsActive] {
            XCTAssertEqual(NetworkResilience.classify(URLError(code)), .offline, "\(code)")
        }
    }

    func test_timeoutIsUnreachableNotOffline() {
        XCTAssertEqual(NetworkResilience.classify(URLError(.timedOut)), .unreachable)
        XCTAssertTrue(NetworkResilience.permitsCachedRead(.unreachable))
        XCTAssertNotEqual(
            NetworkFailureCopy.message(for: URLError(.timedOut), operation: .mutation, otherwise: "x"),
            NetworkFailureCopy.message(for: URLError(.notConnectedToInternet), operation: .mutation, otherwise: "x")
        )
    }

    func test_onlyOfflineMutationCopyClaimsNothingWasSaved() {
        let offline = NetworkFailureCopy.message(
            for: URLError(.notConnectedToInternet), operation: .mutation, otherwise: "x")
        let timedOut = NetworkFailureCopy.message(
            for: URLError(.timedOut), operation: .mutation, otherwise: "x")
        XCTAssertTrue(offline.contains("wasn't saved"), offline)
        XCTAssertFalse(timedOut.contains("wasn't saved"),
                       "a timeout may have been applied with only the reply lost — claiming otherwise invites a duplicate: \(timedOut)")
    }

    func test_cancellationIsNeverOfflineAndNeverRetried() {
        for error in [CancellationError() as Error, URLError(.cancelled) as Error] {
            XCTAssertEqual(NetworkResilience.classify(error), .cancelled)
            XCTAssertFalse(NetworkResilience.permitsCachedRead(.cancelled))
            XCTAssertFalse(NetworkResilience.isRetryableForRead(error))
        }
    }

    func test_clientErrorClassification() {
        XCTAssertEqual(IdentityAPIClientError.classify(URLError(.notConnectedToInternet)), .offline)
        XCTAssertEqual(IdentityAPIClientError.classify(URLError(.timedOut)), .transport)
        XCTAssertEqual(IdentityAPIClientError.classify(URLError(.cancelled)), .cancelled)
        let refusal = IdentityAPIClientError.server(statusCode: 403, message: nil)
        XCTAssertEqual(IdentityAPIClientError.classify(refusal), refusal)
        XCTAssertFalse(refusal.isConnectivityFailure)
        XCTAssertTrue(IdentityAPIClientError.offline.isConnectivityFailure)
        XCTAssertFalse(IdentityAPIClientError.cancelled.isConnectivityFailure)
    }

    // MARK: - Sessions

    func test_apiSession_isBoundedAndFailsFastWhenOffline() {
        for session in [NetworkResilience.apiSession(), NetworkResilience.uploadSession()] {
            let configuration = session.configuration
            XCTAssertEqual(configuration.timeoutIntervalForRequest, NetworkResilience.Timeouts.apiRequest)
            XCTAssertFalse(configuration.waitsForConnectivity,
                           "waitsForConnectivity must be false or an offline request hangs instead of failing")
        }
        XCTAssertEqual(NetworkResilience.apiSession().configuration.timeoutIntervalForResource,
                       NetworkResilience.Timeouts.apiResource)
        XCTAssertEqual(NetworkResilience.uploadSession().configuration.timeoutIntervalForResource,
                       NetworkResilience.Timeouts.uploadResource)
        XCTAssertLessThan(NetworkResilience.Timeouts.apiRequest, 60)
    }

    func test_assetBundleDownloaderSession_alsoFailsFastWhenOffline() {
        XCTAssertFalse(URLSessionAssetBundleDownloader.makeSession().configuration.waitsForConnectivity)
    }

    // MARK: - Case 7 — retries do not duplicate mutations

    func test_getIsRetriedAndBoundedByThePolicy() async {
        CountingFailureProtocol.reset(failWith: URLError(.timedOut))
        let session = CountingFailureProtocol.session()

        var request = URLRequest(url: URL(string: "https://muse.test/read")!)
        request.httpMethod = "GET"
        _ = try? await session.resilientData(for: request)

        XCTAssertEqual(CountingFailureProtocol.attempts, NetworkResilience.RetryPolicy.read.attempts,
                       "a safe read should exhaust the bounded policy, and no more")
    }

    func test_mutationsAreNeverRetried() async {
        for method in ["POST", "PUT", "PATCH", "DELETE"] {
            CountingFailureProtocol.reset(failWith: URLError(.timedOut))
            var request = URLRequest(url: URL(string: "https://muse.test/mutate")!)
            request.httpMethod = method
            _ = try? await CountingFailureProtocol.session().resilientData(for: request)
            XCTAssertEqual(CountingFailureProtocol.attempts, 1,
                           "\(method) must be attempted exactly once — a retried mutation is a duplicated mutation whenever the first attempt landed and only the reply was lost")
        }
    }

    func test_offlineIsNotRetried() async {
        CountingFailureProtocol.reset(failWith: URLError(.notConnectedToInternet))
        var request = URLRequest(url: URL(string: "https://muse.test/read")!)
        request.httpMethod = "GET"
        _ = try? await CountingFailureProtocol.session().resilientData(for: request)
        XCTAssertEqual(CountingFailureProtocol.attempts, 1)
    }

    func test_connectionLostMidRequestIsRetried() async {
        CountingFailureProtocol.reset(failWith: URLError(.networkConnectionLost))
        var request = URLRequest(url: URL(string: "https://muse.test/read")!)
        request.httpMethod = "GET"
        _ = try? await CountingFailureProtocol.session().resilientData(for: request)
        XCTAssertEqual(CountingFailureProtocol.attempts, NetworkResilience.RetryPolicy.read.attempts)
    }

    func test_httpStatusIsReturnedNotRetried() async throws {
        CountingFailureProtocol.reset(respondWith: 500)
        var request = URLRequest(url: URL(string: "https://muse.test/read")!)
        request.httpMethod = "GET"

        let (_, response) = try await CountingFailureProtocol.session().resilientData(for: request)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 500)
        XCTAssertEqual(CountingFailureProtocol.attempts, 1)
    }

    func test_readSucceedsOnASubsequentAttempt() async throws {
        CountingFailureProtocol.reset(failWith: URLError(.timedOut), succeedFromAttempt: 2)
        var request = URLRequest(url: URL(string: "https://muse.test/read")!)
        request.httpMethod = "GET"

        let (data, response) = try await CountingFailureProtocol.session().resilientData(for: request)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
        XCTAssertFalse(data.isEmpty)
        XCTAssertEqual(CountingFailureProtocol.attempts, 2)
    }

    func test_retryBackoffGrowsAndIsBounded() {
        let policy = NetworkResilience.RetryPolicy.read
        let first = policy.delay(forAttempt: 1)
        let second = policy.delay(forAttempt: 2)
        XCTAssertGreaterThan(first, 0)
        XCTAssertGreaterThan(second, first)
        XCTAssertLessThan(second, policy.initialDelay * policy.multiplier * (1 + policy.jitter) + 0.001)
        XCTAssertEqual(NetworkResilience.RetryPolicy.none.attempts, 1)
    }

    func test_withRetry_stopsOnCancellation() async {
        let attempts = Counter()
        let task = Task {
            try await NetworkResilience.withRetry(.read) {
                attempts.increment()
                throw CancellationError()
            }
        }
        let result = await task.result
        XCTAssertThrowsError(try result.get())
        XCTAssertEqual(attempts.value, 1)
    }
}

// MARK: - Support

final class CountingFailureProtocol: URLProtocol {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var failure: Error?
    nonisolated(unsafe) private static var status: Int?
    nonisolated(unsafe) private static var succeedFrom: Int = .max
    nonisolated(unsafe) private static var count = 0

    static func reset(failWith error: Error? = nil, respondWith status: Int? = nil, succeedFromAttempt: Int = .max) {
        lock.lock()
        failure = error
        self.status = status
        succeedFrom = succeedFromAttempt
        count = 0
        lock.unlock()
    }

    static var attempts: Int {
        lock.lock(); defer { lock.unlock() }
        return count
    }

    static func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CountingFailureProtocol.self]
        configuration.waitsForConnectivity = false
        return URLSession(configuration: configuration)
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}

    override func startLoading() {
        Self.lock.lock()
        Self.count += 1
        let attempt = Self.count
        let failure = Self.failure
        let status = Self.status
        let succeedFrom = Self.succeedFrom
        Self.lock.unlock()

        if let failure, attempt < succeedFrom {
            client?.urlProtocol(self, didFailWithError: failure)
            return
        }
        let response = HTTPURLResponse(
            url: request.url!, statusCode: status ?? 200, httpVersion: "HTTP/1.1", headerFields: nil
        )!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data("ok".utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
}

final class Counter: @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0
    func increment() { lock.lock(); count += 1; lock.unlock() }
    var value: Int { lock.lock(); defer { lock.unlock() }; return count }
}
