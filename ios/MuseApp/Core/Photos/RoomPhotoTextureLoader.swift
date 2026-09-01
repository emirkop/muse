import Foundation

public protocol PhotoBytesDownloading: Sendable {
    func download(_ url: URL) async throws -> Data
}

public enum PhotoDownloadError: Error, Equatable {
    case rejected
    case offline
    case transport
}

public struct URLSessionPhotoDownloader: PhotoBytesDownloading {
    private let session: URLSession

    public init(session: URLSession = NetworkResilience.uploadSession()) {
        self.session = session
    }

    public func download(_ url: URL) async throws -> Data {
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.resilientData(for: URLRequest(url: url))
        } catch {
            throw NetworkResilience.classify(error) == .offline
                ? PhotoDownloadError.offline
                : PhotoDownloadError.transport
        }
        guard let http = response as? HTTPURLResponse else { throw PhotoDownloadError.transport }
        switch http.statusCode {
        case 200...299: return data
        case 401, 403: throw PhotoDownloadError.rejected
        default: throw PhotoDownloadError.transport
        }
    }
}

public final class RoomPhotoTextureLoader: RoomPhotoTextureProviding, Sendable {
    private let photoService: RoomPhotoTicketing
    private let downloader: PhotoBytesDownloading
    private let maxConcurrent: Int
    private let now: @Sendable () -> Date
    private let diagnostics: any ErrorReporting

    public init(
        photoService: RoomPhotoTicketing,
        downloader: PhotoBytesDownloading,
        maxConcurrent: Int = RoomPhotoTexturePolicy.maxConcurrentLoads,
        now: @escaping @Sendable () -> Date = { Date() },
        diagnostics: any ErrorReporting = NoErrorReporting()
    ) {
        self.photoService = photoService
        self.downloader = downloader
        self.maxConcurrent = max(1, maxConcurrent)
        self.now = now
        self.diagnostics = diagnostics
    }

    public func textures(
        for placements: [ResolvedPhotoPlacement],
        roomID: String,
        accessToken: String,
        maxLongEdge: Int
    ) -> AsyncStream<RoomPhotoTextureEvent> {
        AsyncStream { continuation in
            let task = Task { [self] in
                await self.run(
                    placements: placements,
                    roomID: roomID,
                    accessToken: accessToken,
                    maxLongEdge: maxLongEdge,
                    emit: { continuation.yield($0) }
                )
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    // MARK: - Pipeline

    private func run(
        placements: [ResolvedPhotoPlacement],
        roomID: String,
        accessToken: String,
        maxLongEdge: Int,
        emit: @Sendable @escaping (RoomPhotoTextureEvent) -> Void
    ) async {
        guard !placements.isEmpty else { return }

        let tickets = TicketStore(
            fetch: { [photoService] in
                try await photoService.fetchPhotoURLs(accessToken: accessToken, roomID: roomID)
            },
            now: now
        )
        do {
            try await tickets.refresh()
        } catch {
            let reason: RoomPhotoLoadFailure = Task.isCancelled ? .cancelled : .noTicket
            for placement in placements { emit(.failed(slotIndex: placement.slotIndex, reason: reason)) }
            return
        }

        for placement in placements {
            if let snapshot = await tickets.snapshot(for: placement.photoAssetID) {
                emit(.dimensions(
                    slotIndex: placement.slotIndex,
                    pixelWidth: snapshot.ticket.pixelWidth,
                    pixelHeight: snapshot.ticket.pixelHeight
                ))
            }
        }
        if Task.isCancelled { return }

        await withTaskGroup(of: Void.self) { group in
            var next = 0
            func enqueue() {
                guard next < placements.count, !Task.isCancelled else { return }
                let placement = placements[next]
                next += 1
                _ = group.addTaskUnlessCancelled { [self] in
                    let event = await self.loadOne(placement, tickets: tickets, maxLongEdge: maxLongEdge)
                    emit(event)
                }
            }
            for _ in 0..<min(maxConcurrent, placements.count) { enqueue() }
            for await _ in group { enqueue() }
        }
    }

    private func loadOne(_ placement: ResolvedPhotoPlacement, tickets: TicketStore, maxLongEdge: Int) async -> RoomPhotoTextureEvent {
        let slot = placement.slotIndex
        if Task.isCancelled { return .failed(slotIndex: slot, reason: .cancelled) }

        guard var snapshot = await tickets.snapshot(for: placement.photoAssetID) else {
            return .failed(slotIndex: slot, reason: .noTicket)
        }

        if snapshot.ticket.expiresAt <= now() {
            guard let fresh = await tickets.snapshot(for: placement.photoAssetID, newerThan: snapshot.generation) else {
                return .failed(slotIndex: slot, reason: .ticketRejected)
            }
            snapshot = fresh
        }

        let data: Data
        do {
            data = try await downloadWithOneRefresh(snapshot, assetID: placement.photoAssetID, tickets: tickets)
        } catch let failure as RoomPhotoLoadFailure {
            return .failed(slotIndex: slot, reason: failure)
        } catch {
            return .failed(slotIndex: slot, reason: Task.isCancelled ? .cancelled : .download)
        }
        if Task.isCancelled { return .failed(slotIndex: slot, reason: .cancelled) }

        do {
            let image = try PhotoTextureDecoder.decode(data, maxLongEdge: maxLongEdge)
            return .decoded(slotIndex: slot, image: image)
        } catch {
            diagnostics.report(ErrorReport(domain: .photoTexture, reason: .decodeFailed))
            return .failed(slotIndex: slot, reason: .decode)
        }
    }

    private func downloadWithOneRefresh(_ snapshot: TicketStore.Snapshot, assetID: String, tickets: TicketStore) async throws -> Data {
        do {
            return try await downloader.download(snapshot.ticket.url)
        } catch PhotoDownloadError.rejected {
            guard let fresh = await tickets.snapshot(for: assetID, newerThan: snapshot.generation) else {
                throw RoomPhotoLoadFailure.ticketRejected
            }
            do {
                return try await downloader.download(fresh.ticket.url)
            } catch PhotoDownloadError.rejected {
                throw RoomPhotoLoadFailure.ticketRejected
            } catch {
                if Task.isCancelled { throw RoomPhotoLoadFailure.cancelled }
                throw RoomPhotoLoadFailure.download
            }
        } catch {
            if Task.isCancelled { throw RoomPhotoLoadFailure.cancelled }
            throw RoomPhotoLoadFailure.download
        }
    }
}

extension RoomPhotoLoadFailure: Error {}

private actor TicketStore {
    struct Snapshot {
        let ticket: PhotoDownloadTicket
        let generation: Int
    }

    private let fetch: @Sendable () async throws -> [PhotoDownloadTicket]
    private let now: @Sendable () -> Date
    private var byAsset: [String: PhotoDownloadTicket] = [:]
    private var generation = 0
    private var inFlight: Task<Void, Error>?

    init(fetch: @escaping @Sendable () async throws -> [PhotoDownloadTicket], now: @escaping @Sendable () -> Date) {
        self.fetch = fetch
        self.now = now
    }

    func snapshot(for assetID: String) -> Snapshot? {
        byAsset[assetID].map { Snapshot(ticket: $0, generation: generation) }
    }

    func refresh() async throws {
        if let inFlight {
            try await inFlight.value
            return
        }
        let task = Task { [fetch] in
            let tickets = try await fetch()
            self.install(tickets)
        }
        inFlight = task
        defer { inFlight = nil }
        try await task.value
    }

    private func install(_ tickets: [PhotoDownloadTicket]) {
        byAsset = Dictionary(tickets.map { ($0.photoAssetID, $0) }, uniquingKeysWith: { _, latest in latest })
        generation += 1
    }

    func snapshot(for assetID: String, newerThan stale: Int) async -> Snapshot? {
        if generation <= stale {
            do {
                try await refresh()
            } catch {
                return nil
            }
        }
        guard generation > stale, let ticket = byAsset[assetID], ticket.expiresAt > now() else { return nil }
        return Snapshot(ticket: ticket, generation: generation)
    }
}
