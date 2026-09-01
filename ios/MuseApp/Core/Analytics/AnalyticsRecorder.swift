import Foundation

public protocol AnalyticsRecording: Sendable {
    func record(_ event: AnalyticsEvent)
}

public struct NoAnalytics: AnalyticsRecording {
    public init() {}
    public func record(_ event: AnalyticsEvent) {}
}

public final class AnalyticsRecorder: AnalyticsRecording {
    private static let bufferLimit = 20
    private static let batchLimit = 50

    private let client: any AnalyticsSubmitting
    private let accessToken: @Sendable () async -> String?
    private let newUUID: @Sendable () -> String
    private let buffer = Buffer()

    public init(
        client: any AnalyticsSubmitting,
        accessToken: @escaping @Sendable () async -> String?,
        newUUID: @escaping @Sendable () -> String = { UUID().uuidString.lowercased() }
    ) {
        self.client = client
        self.accessToken = accessToken
        self.newUUID = newUUID
    }

    public func record(_ event: AnalyticsEvent) {
        let payload = event.payload(uuid: newUUID())
        Task.detached { [buffer, client, accessToken] in
            let batch = await buffer.append(payload, limit: Self.bufferLimit)
            guard !batch.isEmpty else { return }
            guard let token = await accessToken() else {
                return
            }
            await client.submit(events: batch, accessToken: token)
        }
    }

    private actor Buffer {
        private var pending: [AnalyticsEventPayload] = []

        func append(_ payload: AnalyticsEventPayload, limit: Int) -> [AnalyticsEventPayload] {
            pending.append(payload)
            if pending.count > limit {
                pending.removeFirst(pending.count - limit)
            }
            let batch = pending
            pending.removeAll()
            return batch
        }
    }
}

public protocol AnalyticsSubmitting: Sendable {
    func submit(events: [AnalyticsEventPayload], accessToken: String) async
}

public struct AnalyticsAPIClient: AnalyticsSubmitting {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = NetworkResilience.apiSession()) {
        self.baseURL = baseURL
        self.session = session
    }

    public func submit(events: [AnalyticsEventPayload], accessToken: String) async {
        guard !events.isEmpty else { return }
        var request = URLRequest(url: baseURL.appendingPathComponent("analytics/events"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        guard let body = try? JSONEncoder().encode(RequestBody(events: events)) else { return }
        request.httpBody = body

        _ = try? await session.resilientData(for: request)
    }

    private struct RequestBody: Encodable {
        let events: [AnalyticsEventPayload]
    }
}
