import XCTest
@testable import MuseApp

@MainActor
final class RoomContentEditCoordinatorTests: XCTestCase {

    private let table = PlaceholderRoomSlotTable.build()

    private func room(_ count: Int) -> Room {
        Room(
            id: "room_1", name: "Trabzon", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: "caption \($0)") }
        )
    }

    private func makeCoordinator(
        photoCount: Int = 4,
        service: SpyReorderService? = nil,
        rooms: FakeMuseumService? = nil
    ) -> (RoomContentEditCoordinator, SpyReorderService, FakeMuseumService) {
        let room = room(photoCount)
        let spy = service ?? SpyReorderService(room: room)
        let museum = rooms ?? FakeMuseumService()
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            fatalError("fixture must resolve")
        }
        let coordinator = RoomContentEditCoordinator(
            room: room, slotTable: table, placements: placements,
            photoService: spy, roomService: museum, accessToken: "tok"
        )
        return (coordinator, spy, museum)
    }

    private func order(_ coordinator: RoomContentEditCoordinator) -> [String] {
        RoomPhotoOrder.assetIDs(coordinator.room.photoSlots)
    }

    private func settle() async {
        for _ in 0..<12 { await Task.yield() }
    }

    // MARK: - Optimistic apply

    func test_swap_appliesLocallyAndImmediately_beforeAnyResponse() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.isGated = true
        var emitted: [[ResolvedPhotoPlacement]] = []
        coordinator.onPlacementsChanged = { emitted.append($0) }

        XCTAssertTrue(coordinator.swap(from: 0, to: 3))

        XCTAssertEqual(order(coordinator), ["asset_3", "asset_1", "asset_2", "asset_0"])
        XCTAssertEqual(coordinator.status, .saving)
        XCTAssertEqual(emitted.count, 1)
        XCTAssertEqual(emitted[0].map(\.photoAssetID), ["asset_3", "asset_1", "asset_2", "asset_0"])

        spy.releaseAll()
        await settle()
        XCTAssertEqual(coordinator.status, .saved)
    }

    func test_swap_placementsMatchThePlacementEngineExactly() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: Room.maxPhotos)
        coordinator.swap(from: 0, to: 27)

        guard case .resolved(let expected) = RoomPlacementResolver.resolve(room: coordinator.room, slotTable: table) else {
            return XCTFail("must resolve")
        }
        XCTAssertEqual(coordinator.placements, expected)
        XCTAssertEqual(coordinator.placements[0].anchor.wall, .focal)
        XCTAssertEqual(coordinator.placements[0].photoAssetID, "asset_27")
        spy.releaseAll()
        await settle()
    }

    func test_swap_sendsTheCompleteOrder_asAssetIDs() async {
        let (coordinator, spy, _) = makeCoordinator()
        coordinator.swap(from: 1, to: 2)
        await settle()

        XCTAssertEqual(spy.orders.count, 1)
        XCTAssertEqual(spy.orders[0], ["asset_0", "asset_2", "asset_1", "asset_3"])
    }

    func test_noOpSwap_changesNothing_andSendsNothing() async {
        let (coordinator, spy, _) = makeCoordinator()

        XCTAssertFalse(coordinator.swap(from: 2, to: 2))
        XCTAssertFalse(coordinator.swap(from: 0, to: 99))
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_0", "asset_1", "asset_2", "asset_3"])
        XCTAssertEqual(coordinator.status, .idle)
        XCTAssertTrue(spy.orders.isEmpty, "a no-op must not hit the server")
    }

    func test_singlePhotoRoom_cannotSwap() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 1)
        XCTAssertFalse(coordinator.swap(from: 0, to: 0))
        await settle()
        XCTAssertTrue(spy.orders.isEmpty)
    }

    // MARK: - Success convergence

    func test_success_convergesSilentlyWhenServerAgrees() async {
        let (coordinator, spy, _) = makeCoordinator()
        var emissions = 0
        coordinator.onPlacementsChanged = { _ in emissions += 1 }

        coordinator.swap(from: 0, to: 1)
        await settle()

        XCTAssertEqual(coordinator.status, .saved)
        XCTAssertEqual(order(coordinator), ["asset_1", "asset_0", "asset_2", "asset_3"])
        XCTAssertEqual(emissions, 1, "the server agreed — no second relayout")
    }

    func test_success_convergesToServerOrderWhenItDiffers() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.nextResults = [.success(spy.slots(for: ["asset_3", "asset_2", "asset_1", "asset_0"]))]

        coordinator.swap(from: 0, to: 1)
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_3", "asset_2", "asset_1", "asset_0"], "the server is authoritative")
        XCTAssertEqual(coordinator.placements.map(\.photoAssetID), ["asset_3", "asset_2", "asset_1", "asset_0"])
        XCTAssertEqual(coordinator.status, .saved)
    }

    func test_success_preservesCaptionsFromTheServer() async {
        let (coordinator, spy, _) = makeCoordinator()
        coordinator.swap(from: 0, to: 2)
        await settle()

        for slot in coordinator.room.photoSlots {
            XCTAssertEqual(slot.caption, "caption \(slot.photoAssetID.dropFirst("asset_".count))")
        }
        for placement in coordinator.placements {
            XCTAssertEqual(placement.caption, "caption \(placement.photoAssetID.dropFirst("asset_".count))")
        }
    }

    // MARK: - Rollback

    func test_transportFailure_restoresThePreviousOrderExactly() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.nextResults = [.failure(IdentityAPIClientError.transport)]
        var emitted: [[String]] = []
        coordinator.onPlacementsChanged = { emitted.append($0.map(\.photoAssetID)) }

        coordinator.swap(from: 0, to: 3)
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_0", "asset_1", "asset_2", "asset_3"], "previous order restored")
        XCTAssertEqual(coordinator.status, .failedTransport)
        XCTAssertNotNil(coordinator.statusMessage)
        XCTAssertEqual(emitted.last, ["asset_0", "asset_1", "asset_2", "asset_3"], "the scene was re-laid out to the restored order")
    }

    func test_invalidOrder400_rollsBackAndDoesNotRetry() async {
        let (coordinator, spy, _) = makeCoordinator()
        spy.nextResults = [.failure(PhotoAPIError(statusCode: 400, message: nil, code: "invalid_order", assetID: nil))]

        coordinator.swap(from: 0, to: 1)
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_0", "asset_1", "asset_2", "asset_3"])
        XCTAssertEqual(coordinator.status, .failedInvalid)
        XCTAssertEqual(spy.orders.count, 1, "a client-side correctness failure must not be retried or massaged")
    }

    func test_orderMismatch409_rollsBackAndReloadsAuthoritativeRoom() async {
        let (coordinator, spy, museum) = makeCoordinator()
        spy.nextResults = [.failure(PhotoAPIError(statusCode: 409, message: nil, code: "order_mismatch", assetID: nil))]
        let authoritative = Room(
            id: "room_1", name: "Trabzon", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: [
                PhotoSlotAssignment(slotIndex: 0, photoAssetID: "asset_2", caption: "caption 2"),
                PhotoSlotAssignment(slotIndex: 1, photoAssetID: "asset_3", caption: "caption 3"),
                PhotoSlotAssignment(slotIndex: 2, photoAssetID: "asset_0", caption: "caption 0"),
                PhotoSlotAssignment(slotIndex: 3, photoAssetID: "asset_1", caption: "caption 1")
            ]
        )
        museum.roomsResult = .success([authoritative])

        coordinator.swap(from: 0, to: 1)
        await settle()

        XCTAssertEqual(coordinator.status, .failedStale)
        XCTAssertEqual(museum.fetchRoomCallCount, 1, "the authoritative Room must be reloaded")
        XCTAssertEqual(order(coordinator), ["asset_2", "asset_3", "asset_0", "asset_1"], "the server's current order is rendered")
        XCTAssertEqual(coordinator.placements.map(\.photoAssetID), ["asset_2", "asset_3", "asset_0", "asset_1"])
        XCTAssertEqual(spy.orders.count, 1, "the stale order must not be resubmitted")
    }

    func test_orderMismatch_withFailingReload_stillLeavesARolledBackOrder() async {
        let (coordinator, spy, museum) = makeCoordinator()
        spy.nextResults = [.failure(PhotoAPIError(statusCode: 409, message: nil, code: "order_mismatch", assetID: nil))]
        museum.roomsResult = .failure(IdentityAPIClientError.transport)

        coordinator.swap(from: 0, to: 1)
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_0", "asset_1", "asset_2", "asset_3"], "rollback stands even if the reload fails")
        XCTAssertEqual(coordinator.status, .failedStale)
    }

    func test_rollbackTargetsTheLastServerConfirmedOrder() async {
        let (coordinator, spy, _) = makeCoordinator()

        coordinator.swap(from: 0, to: 1)
        await settle()
        XCTAssertEqual(order(coordinator), ["asset_1", "asset_0", "asset_2", "asset_3"])

        spy.nextResults = [.failure(IdentityAPIClientError.transport)]
        coordinator.swap(from: 2, to: 3)
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_1", "asset_0", "asset_2", "asset_3"], "back to the confirmed order, not the original")
    }

    // MARK: - Rapid reorders

    func test_rapidSwaps_serializeAndCoalesce_withoutCorruptingLocalOrder() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 5)
        spy.isGated = true

        coordinator.swap(from: 0, to: 1)
        await settle()
        XCTAssertEqual(spy.orders.count, 1, "the first swap is on the wire")

        coordinator.swap(from: 1, to: 2)
        coordinator.swap(from: 3, to: 4)
        await settle()

        let expectedLocal = order(coordinator)
        XCTAssertEqual(spy.orders.count, 1, "only one request may be in flight at a time")

        spy.releaseAll()
        await settle()

        XCTAssertEqual(spy.orders.count, 2, "the queued swaps became exactly one follow-up request")
        XCTAssertEqual(spy.orders[1], expectedLocal, "the follow-up carries the latest complete order")
        XCTAssertEqual(order(coordinator), expectedLocal, "local order is intact")
        XCTAssertEqual(coordinator.status, .saved)
    }

    func test_swapsMadeBackToBack_coalesceIntoOneRequest() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 5)

        coordinator.swap(from: 0, to: 1)
        coordinator.swap(from: 1, to: 2)
        coordinator.swap(from: 3, to: 4)
        let expectedLocal = order(coordinator)
        await settle()

        XCTAssertEqual(spy.orders.count, 1, "back-to-back swaps need only one request")
        XCTAssertEqual(spy.orders[0], expectedLocal, "and it carries the final order")
        XCTAssertEqual(order(coordinator), expectedLocal)
        XCTAssertEqual(coordinator.status, .saved)
    }

    func test_staleSuccessResponse_doesNotClobberANewerLocalOrder() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 5)
        spy.isGated = true

        coordinator.swap(from: 0, to: 1)
        await settle()
        coordinator.swap(from: 2, to: 3)
        let newerLocal = order(coordinator)

        spy.nextResults = [.success(spy.slots(for: ["asset_1", "asset_0", "asset_2", "asset_3", "asset_4"]))]
        spy.releaseOne()
        await settle()

        XCTAssertEqual(order(coordinator), newerLocal, "the newer optimistic order must survive an older confirmation")
        spy.releaseAll()
        await settle()
    }

    func test_manyRapidSwaps_endInAConsistentContiguousOrder() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: Room.maxPhotos)
        for index in 0..<20 {
            coordinator.swap(from: index % Room.maxPhotos, to: (index * 7 + 3) % Room.maxPhotos)
        }
        await settle()
        spy.releaseAll()
        await settle()

        let slots = RoomPhotoOrder.normalised(coordinator.room.photoSlots)
        XCTAssertEqual(slots.count, Room.maxPhotos)
        XCTAssertEqual(Set(slots.map(\.photoAssetID)).count, Room.maxPhotos, "no duplicates or losses")
        XCTAssertEqual(slots.map(\.slotIndex), Array(0..<Room.maxPhotos), "contiguous")
        XCTAssertEqual(coordinator.placements.count, Room.maxPhotos)
    }

    // MARK: - No media work

    func test_reordering_neverUploadsOrRequestsDeliveryTickets() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 6)
        for index in 0..<5 {
            coordinator.swap(from: index, to: index + 1)
        }
        await settle()
        spy.releaseAll()
        await settle()

        XCTAssertEqual(spy.uploadInitiations, 0, "no upload may be caused by reordering")
        XCTAssertEqual(spy.assignCalls, 0, "no photo assignment may be caused by reordering")
        XCTAssertEqual(spy.deliveryTicketRequests, 0, "no delivery/download may be caused by reordering")
    }
}

@MainActor
final class SpyReorderService: RoomPhotoServicing {
    private(set) var orders: [[String]] = []
    private(set) var captionCalls: [(assetID: String, caption: String)] = []
    private(set) var uploadInitiations = 0
    private(set) var assignCalls = 0
    private(set) var deliveryTicketRequests = 0

    var nextResults: [Result<[PhotoSlotAssignment], Error>] = []
    var isGated = false
    var nextCaptionResults: [Result<[PhotoSlotAssignment], Error>] = []
    var isCaptionGated = false

    private var captions: [String: String]
    private var currentOrder: [String]
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var captionWaiters: [CheckedContinuation<Void, Never>] = []

    init(room: Room) {
        let ordered = RoomPhotoOrder.normalised(room.photoSlots)
        captions = Dictionary(uniqueKeysWithValues: ordered.map { ($0.photoAssetID, $0.caption) })
        currentOrder = ordered.map(\.photoAssetID)
    }

    var serverSlots: [PhotoSlotAssignment] { slots(for: currentOrder) }

    func slots(for order: [String]) -> [PhotoSlotAssignment] {
        order.enumerated().map { position, assetID in
            PhotoSlotAssignment(slotIndex: position, photoAssetID: assetID, caption: captions[assetID] ?? "")
        }
    }

    func releaseOne() {
        guard !waiters.isEmpty else { return }
        waiters.removeFirst().resume()
    }

    func releaseAll() {
        isGated = false
        let pending = waiters
        waiters.removeAll()
        for waiter in pending { waiter.resume() }
        releaseCaptions()
        releaseDeletes()
    }

    func releaseCaptions() {
        isCaptionGated = false
        let pending = captionWaiters
        captionWaiters.removeAll()
        for waiter in pending { waiter.resume() }
    }

    func applyReplacement(of assetID: String, with replacementAssetID: String) -> [PhotoSlotAssignment] {
        if let index = currentOrder.firstIndex(of: assetID) {
            currentOrder[index] = replacementAssetID
            captions[replacementAssetID] = captions.removeValue(forKey: assetID) ?? ""
        }
        return slots(for: currentOrder)
    }

    // MARK: RoomPhotoServicing

    func reorderPhotos(accessToken: String, roomID: String, orderedAssetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        orders.append(orderedAssetIDs)
        if isGated {
            await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                waiters.append(continuation)
            }
        }
        if nextResults.isEmpty {
            guard orderedAssetIDs.count == currentOrder.count, Set(orderedAssetIDs) == Set(currentOrder) else {
                throw PhotoAPIError(statusCode: 409, message: nil, code: "order_mismatch", assetID: nil)
            }
            currentOrder = orderedAssetIDs
            return slots(for: orderedAssetIDs)
        }
        let result = try nextResults.removeFirst().get()
        currentOrder = RoomPhotoOrder.assetIDs(result)
        return result
    }

    func setPhotoCaption(accessToken: String, roomID: String, photoAssetID: String, caption: String) async throws -> [PhotoSlotAssignment] {
        captionCalls.append((photoAssetID, caption))
        if isCaptionGated {
            await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                captionWaiters.append(continuation)
            }
        }
        if nextCaptionResults.isEmpty {
            captions[photoAssetID] = caption
            return slots(for: currentOrder)
        }
        return try nextCaptionResults.removeFirst().get()
    }

    func initiateUpload(accessToken: String, declaration: PhotoUploadDeclaration) async throws -> PhotoUploadTicket {
        uploadInitiations += 1
        throw IdentityAPIClientError.invalidResponse
    }

    func assignPhotos(accessToken: String, roomID: String, assetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        assignCalls += 1
        throw IdentityAPIClientError.invalidResponse
    }

    private(set) var replaceCalls = 0
    func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, replacementAssetID: String) async throws -> [PhotoSlotAssignment] {
        replaceCalls += 1
        throw IdentityAPIClientError.invalidResponse
    }

    private(set) var deleteCalls: [String] = []
    var nextDeleteResults: [Result<[PhotoSlotAssignment], Error>] = []
    var isDeleteGated = false
    private var deleteWaiters: [CheckedContinuation<Void, Never>] = []

    func releaseDeletes() {
        isDeleteGated = false
        let pending = deleteWaiters
        deleteWaiters.removeAll()
        for waiter in pending { waiter.resume() }
    }

    func deletePhoto(accessToken: String, roomID: String, photoAssetID: String) async throws -> [PhotoSlotAssignment] {
        deleteCalls.append(photoAssetID)
        if isDeleteGated {
            await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                deleteWaiters.append(continuation)
            }
        }
        if nextDeleteResults.isEmpty {
            guard currentOrder.contains(photoAssetID) else {
                throw PhotoAPIError(statusCode: 404, message: nil, code: "photo_not_in_room", assetID: photoAssetID)
            }
            currentOrder.removeAll { $0 == photoAssetID }
            captions.removeValue(forKey: photoAssetID)
            return slots(for: currentOrder)
        }
        return try nextDeleteResults.removeFirst().get()
    }

    func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] {
        deliveryTicketRequests += 1
        return []
    }
}
