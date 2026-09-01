import XCTest
@testable import MuseApp

final class RoomPhotoTextureLoaderTests: XCTestCase {

    private func placements(_ count: Int) -> [ResolvedPhotoPlacement] {
        (0..<count).map { slot in
            ResolvedPhotoPlacement(
                slotIndex: slot, photoAssetID: "asset_\(slot)", caption: "",
                anchor: SlotAnchor(wall: .left, positionOnWall: slot),
                transform: SlotTransform(position: .zero)
            )
        }
    }

    private func collect(_ stream: AsyncStream<RoomPhotoTextureEvent>) async -> [RoomPhotoTextureEvent] {
        var events: [RoomPhotoTextureEvent] = []
        for await event in stream { events.append(event) }
        return events
    }

    private func decodedSlots(_ events: [RoomPhotoTextureEvent]) -> Set<Int> {
        Set(events.compactMap { if case .decoded(let s, _) = $0 { return s } else { return nil } })
    }

    private func failures(_ events: [RoomPhotoTextureEvent]) -> [Int: RoomPhotoLoadFailure] {
        var out: [Int: RoomPhotoLoadFailure] = [:]
        for event in events { if case .failed(let s, let r) = event { out[s] = r } }
        return out
    }

    // MARK: - Happy path

    func test_emitsDimensionsFirst_thenDecodesEveryPhoto_keyedBySlot() async {
        let service = FakeTicketService(count: 5)
        let downloader = FakeBytesDownloader()
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: downloader)

        let events = await collect(loader.textures(for: placements(5), roomID: "r", accessToken: "t", maxLongEdge: 512))

        let firstDecodeIndex = events.firstIndex { if case .decoded = $0 { return true } else { return false } }!
        let dimensionCount = events.prefix(firstDecodeIndex).filter { if case .dimensions = $0 { return true } else { return false } }.count
        XCTAssertEqual(dimensionCount, 5, "every plane must be sizable before any byte is downloaded")

        XCTAssertEqual(decodedSlots(events), [0, 1, 2, 3, 4])
        XCTAssertTrue(failures(events).isEmpty)
        for event in events {
            if case .decoded(let slot, let image) = event {
                XCTAssertEqual(image.pixelWidth, 100 + slot, "slot \(slot) received another slot's bytes")
            }
        }
        let fetches = await service.fetchCount
        XCTAssertEqual(fetches, 1)
    }

    // MARK: - Out-of-order completion

    func test_outOfOrderCompletion_stillMapsEachImageToItsSlot() async {
        let service = FakeTicketService(count: 6)
        let downloader = FakeBytesDownloader(delayNanosecondsForSlot: { slot in UInt64((6 - slot) * 3_000_000) })
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: downloader, maxConcurrent: 6)

        let events = await collect(loader.textures(for: placements(6), roomID: "r", accessToken: "t", maxLongEdge: 512))

        let decodeOrder = events.compactMap { if case .decoded(let s, _) = $0 { return s } else { return nil } }
        XCTAssertEqual(Set(decodeOrder), [0, 1, 2, 3, 4, 5])
        XCTAssertNotEqual(decodeOrder, [0, 1, 2, 3, 4, 5], "the test must actually exercise out-of-order arrival")
        for event in events {
            if case .decoded(let slot, let image) = event {
                XCTAssertEqual(image.pixelWidth, 100 + slot)
            }
        }
    }

    // MARK: - Concurrency bound

    func test_neverExceedsTheConcurrencyBound() async {
        let service = FakeTicketService(count: 12)
        let downloader = FakeBytesDownloader(delayNanosecondsForSlot: { _ in 5_000_000 })
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: downloader, maxConcurrent: 3)

        _ = await collect(loader.textures(for: placements(12), roomID: "r", accessToken: "t", maxLongEdge: 512))

        let peak = await downloader.peakInFlight
        XCTAssertGreaterThan(peak, 1)
        XCTAssertLessThanOrEqual(peak, 3)
    }

    // MARK: - Tickets

    func test_missingTicket_failsOnlyThatSlot() async {
        let service = FakeTicketService(count: 3, omitAssets: ["asset_1"])
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: FakeBytesDownloader())

        let events = await collect(loader.textures(for: placements(3), roomID: "r", accessToken: "t", maxLongEdge: 512))

        XCTAssertEqual(failures(events), [1: .noTicket])
        XCTAssertEqual(decodedSlots(events), [0, 2])
    }

    func test_expiredTicket_isRefreshedBeforeDownload() async {
        let service = FakeTicketService(count: 2, firstBatchExpired: true)
        let downloader = FakeBytesDownloader()
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: downloader)

        let events = await collect(loader.textures(for: placements(2), roomID: "r", accessToken: "t", maxLongEdge: 512))

        XCTAssertEqual(decodedSlots(events), [0, 1])
        let fetches = await service.fetchCount
        XCTAssertEqual(fetches, 2, "two concurrent expiries must coalesce into exactly one refresh")
        for url in await downloader.requested {
            XCTAssertFalse(url.absoluteString.contains("gen=1"), "the expired (gen=1) URL must never be downloaded: \(url)")
            XCTAssertTrue(url.absoluteString.contains("gen=2"), "both must use the single refreshed generation: \(url)")
        }
    }

    func test_rejectedTicket_refreshesOnce_andRetriesOnce() async {
        let service = FakeTicketService(count: 1)
        let downloader = FakeBytesDownloader(rejectURLsContaining: "gen=1")
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: downloader)

        let events = await collect(loader.textures(for: placements(1), roomID: "r", accessToken: "t", maxLongEdge: 512))

        XCTAssertEqual(decodedSlots(events), [0])
        let fetches = await service.fetchCount
        let requested = await downloader.requested
        XCTAssertEqual(fetches, 2)
        XCTAssertEqual(requested.count, 2, "exactly one retry")
    }

    func test_ticketRejectedTwice_failsThatSlotOnly() async {
        let service = FakeTicketService(count: 2)
        let downloader = FakeBytesDownloader(rejectURLsContaining: "asset_0")
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: downloader)

        let events = await collect(loader.textures(for: placements(2), roomID: "r", accessToken: "t", maxLongEdge: 512))

        XCTAssertEqual(failures(events), [0: .ticketRejected])
        XCTAssertEqual(decodedSlots(events), [1])
        let attemptsForSlot0 = await downloader.requested.filter { $0.absoluteString.contains("asset_0") }.count
        XCTAssertEqual(attemptsForSlot0, 2, "one refresh, one retry, then stop")
    }

    func test_ticketFetchFailure_failsEverySlot_withoutThrowing() async {
        let service = FakeTicketService(count: 3, failFetch: true)
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: FakeBytesDownloader())

        let events = await collect(loader.textures(for: placements(3), roomID: "r", accessToken: "t", maxLongEdge: 512))

        XCTAssertEqual(failures(events).count, 3)
        XCTAssertTrue(failures(events).values.allSatisfy { $0 == .noTicket })
    }

    // MARK: - Per-photo failures

    func test_downloadFailure_andDecodeFailure_areIsolated() async {
        let service = FakeTicketService(count: 4)
        let downloader = FakeBytesDownloader(transportFailFor: ["asset_1"], garbageFor: ["asset_2"])
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: downloader)

        let events = await collect(loader.textures(for: placements(4), roomID: "r", accessToken: "t", maxLongEdge: 512))

        XCTAssertEqual(failures(events), [1: .download, 2: .decode])
        XCTAssertEqual(decodedSlots(events), [0, 3])
    }

    // MARK: - Cancellation

    func test_cancellingTheConsumingTask_stopsTheLoad_andNoEventsArriveAfterwards() async {
        let service = FakeTicketService(count: 10)
        let downloader = FakeBytesDownloader(delayNanosecondsForSlot: { _ in 40_000_000 })
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: downloader, maxConcurrent: 2)
        let stream = loader.textures(for: placements(10), roomID: "r", accessToken: "t", maxLongEdge: 512)

        let received = ReceivedEvents()
        let consumer = Task {
            for await event in stream {
                if case .decoded = event { await received.increment() }
            }
        }
        let deadline = Date().addingTimeInterval(5)
        while await received.count < 2, Date() < deadline {
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        consumer.cancel()
        await consumer.value
        let countAtCancel = await received.count
        try? await Task.sleep(nanoseconds: 300_000_000)

        XCTAssertGreaterThanOrEqual(countAtCancel, 2)
        let finalCount = await received.count
        let requested = await downloader.requested
        XCTAssertEqual(finalCount, countAtCancel, "no events may be delivered after cancellation")
        XCTAssertLessThan(requested.count, 10, "remaining downloads must have been cancelled, not drained (\(requested.count) requested)")
    }

    func test_emptyPlacements_produceNoEventsAndNoFetch() async {
        let service = FakeTicketService(count: 0)
        let loader = RoomPhotoTextureLoader(photoService: service, downloader: FakeBytesDownloader())

        let events = await collect(loader.textures(for: [], roomID: "r", accessToken: "t", maxLongEdge: 512))

        XCTAssertTrue(events.isEmpty)
        let fetches = await service.fetchCount
        XCTAssertEqual(fetches, 0)
    }
}

// MARK: - Fakes

actor ReceivedEvents {
    private(set) var count = 0
    func increment() { count += 1 }
}

actor FakeTicketService: RoomPhotoServicing {
    private let count: Int
    private let omitAssets: Set<String>
    private let firstBatchExpired: Bool
    private let failFetch: Bool
    private(set) var fetchCount = 0

    init(count: Int, omitAssets: Set<String> = [], firstBatchExpired: Bool = false, failFetch: Bool = false) {
        self.count = count
        self.omitAssets = omitAssets
        self.firstBatchExpired = firstBatchExpired
        self.failFetch = failFetch
    }

    func initiateUpload(accessToken: String, declaration: PhotoUploadDeclaration) async throws -> PhotoUploadTicket {
        fatalError("not used")
    }
    func assignPhotos(accessToken: String, roomID: String, assetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        fatalError("not used")
    }
    func reorderPhotos(accessToken: String, roomID: String, orderedAssetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        fatalError("not used")
    }

    func setPhotoCaption(accessToken: String, roomID: String, photoAssetID: String, caption: String) async throws -> [PhotoSlotAssignment] {
        throw IdentityAPIClientError.invalidResponse
    }

    func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, replacementAssetID: String) async throws -> [PhotoSlotAssignment] {
        throw IdentityAPIClientError.invalidResponse
    }

    func deletePhoto(accessToken: String, roomID: String, photoAssetID: String) async throws -> [PhotoSlotAssignment] {
        throw IdentityAPIClientError.invalidResponse
    }

    func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] {
        fetchCount += 1
        if failFetch { throw IdentityAPIClientError.transport }
        let expired = firstBatchExpired && fetchCount == 1
        return (0..<count).compactMap { slot in
            let asset = "asset_\(slot)"
            if omitAssets.contains(asset) { return nil }
            return PhotoDownloadTicket(
                photoAssetID: asset,
                url: URL(string: "https://store.test/\(asset)?gen=\(fetchCount)")!,
                expiresAt: Date().addingTimeInterval(expired ? -60 : 300),
                pixelWidth: 100 + slot, pixelHeight: 50
            )
        }
    }
}

actor FakeBytesDownloader: PhotoBytesDownloading {
    private let delayNanosecondsForSlot: @Sendable (Int) -> UInt64
    private let rejectURLsContaining: String?
    private let transportFailFor: Set<String>
    private let garbageFor: Set<String>
    private(set) var requested: [URL] = []
    private(set) var peakInFlight = 0
    private var inFlight = 0

    init(
        delayNanosecondsForSlot: @escaping @Sendable (Int) -> UInt64 = { _ in 0 },
        rejectURLsContaining: String? = nil,
        transportFailFor: Set<String> = [],
        garbageFor: Set<String> = []
    ) {
        self.delayNanosecondsForSlot = delayNanosecondsForSlot
        self.rejectURLsContaining = rejectURLsContaining
        self.transportFailFor = transportFailFor
        self.garbageFor = garbageFor
    }

    func download(_ url: URL) async throws -> Data {
        requested.append(url)
        let asset = url.lastPathComponent
        let slot = Int(asset.split(separator: "_").last!)!
        inFlight += 1
        peakInFlight = max(peakInFlight, inFlight)
        defer { inFlight -= 1 }

        let delay = delayNanosecondsForSlot(slot)
        if delay > 0 { try await Task.sleep(nanoseconds: delay) }
        if let needle = rejectURLsContaining, url.absoluteString.contains(needle) { throw PhotoDownloadError.rejected }
        if transportFailFor.contains(asset) { throw PhotoDownloadError.transport }
        if garbageFor.contains(asset) { return Data("not a jpeg".utf8) }
        return PhotoTextureDecoderTests.jpeg(width: 100 + slot, height: 50)
    }
}
