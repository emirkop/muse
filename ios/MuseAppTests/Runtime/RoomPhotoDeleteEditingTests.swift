import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RoomPhotoDeleteEditingTests: XCTestCase {

    private let table = PlaceholderRoomSlotTable.build()

    private func room(_ count: Int, captions: [Int: String] = [:]) -> Room {
        Room(
            id: "room_1", name: "Trabzon", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: (0..<count).map {
                PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: captions[$0] ?? "")
            }
        )
    }

    private func makeCoordinator(
        photoCount: Int = 5,
        captions: [Int: String] = [:]
    ) -> (RoomContentEditCoordinator, SpyReorderService, FakeMuseumService, FakeReplacer) {
        let room = room(photoCount, captions: captions)
        let spy = SpyReorderService(room: room)
        let museum = FakeMuseumService()
        let replacer = FakeReplacer(server: spy)
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            fatalError("fixture must resolve")
        }
        let coordinator = RoomContentEditCoordinator(
            room: room, slotTable: table, placements: placements,
            photoService: spy, roomService: museum, accessToken: "tok", photoReplacer: replacer
        )
        return (coordinator, spy, museum, replacer)
    }

    private func order(_ coordinator: RoomContentEditCoordinator) -> [String] {
        RoomPhotoOrder.assetIDs(coordinator.room.photoSlots)
    }

    private func settle() async {
        for _ in 0..<12 { await Task.yield() }
    }

    private func picked(_ id: String = "picked_1") -> PickedPhoto {
        let file = NormalizedPhotoFile(
            fileURL: URL(fileURLWithPath: "/tmp/\(id).jpg"), contentType: "image/jpeg", byteSize: 64,
            pixelWidth: 1200, pixelHeight: 800, sha256Hex: String(repeating: "a", count: 64)
        )
        return PickedPhoto(id: id, assetIdentifier: nil, loadState: .ready(thumbnail: Data([1]), file: file))
    }

    // MARK: - Optimistic removal and compaction

    func test_delete_removesOptimistically_compactsAndReflows_thenConfirms() async {
        let (coordinator, spy, _, _) = makeCoordinator(captions: [3: "kept"])
        var events: [String] = []
        coordinator.onPhotoRemovalBegan = { events.append("began \($0)") }
        coordinator.onPhotoRemovalCommitted = { events.append("committed \($0)") }
        coordinator.onPhotoRemovalReverted = { events.append("reverted \($0)") }
        coordinator.onPlacementsChanged = { events.append("relayout \($0.count)") }
        XCTAssertFalse(coordinator.placements.contains { $0.anchor.wall == .rear }, "five photographs: no rear wall")

        let outcome = await coordinator.deletePhoto(assetID: "asset_1")

        XCTAssertEqual(outcome, .deleted)
        XCTAssertEqual(order(coordinator), ["asset_0", "asset_2", "asset_3", "asset_4"])
        XCTAssertEqual(coordinator.room.photoSlots.map(\.slotIndex), [0, 1, 2, 3], "compacted — no gap")
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_3"), "kept", "captions stay with their photographs")
        XCTAssertNil(coordinator.caption(forAssetID: "asset_1"))
        XCTAssertTrue(coordinator.placements.contains { $0.anchor.wall == .rear }, "four photographs: the layout reflowed")
        XCTAssertEqual(events, ["began asset_1", "relayout 4", "committed asset_1"])
        XCTAssertEqual(spy.deleteCalls, ["asset_1"])
    }

    func test_delete_isVisibleBeforeTheServerAnswers() async {
        let (coordinator, spy, _, _) = makeCoordinator()
        spy.isDeleteGated = true

        let task = Task { await coordinator.deletePhoto(assetID: "asset_2") }
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_0", "asset_1", "asset_3", "asset_4"], "gone locally already")
        XCTAssertTrue(coordinator.isDeleting(assetID: "asset_2"))
        XCTAssertEqual(coordinator.placements.count, 4)

        spy.releaseDeletes()
        let outcome = await task.value
        XCTAssertEqual(outcome, .deleted)
        XCTAssertFalse(coordinator.isDeleting(assetID: "asset_2"))
    }

    func test_delete_theServersSlotsWin_whenTheyDiffer() async {
        let (coordinator, spy, _, _) = makeCoordinator(photoCount: 4)
        spy.nextDeleteResults = [.success(spy.slots(for: ["asset_3", "asset_2", "asset_0"]))]

        _ = await coordinator.deletePhoto(assetID: "asset_1")

        XCTAssertEqual(order(coordinator), ["asset_3", "asset_2", "asset_0"], "the server is authoritative")
    }

    // MARK: - Failure puts the photograph back

    func test_transportFailure_restoresThePhotographExactly_andReportsIt() async {
        let (coordinator, spy, _, _) = makeCoordinator(captions: [1: "mine"])
        spy.nextDeleteResults = [.failure(IdentityAPIClientError.transport)]
        var events: [String] = []
        coordinator.onPhotoRemovalBegan = { events.append("began \($0)") }
        coordinator.onPhotoRemovalReverted = { events.append("reverted \($0)") }
        coordinator.onPhotoRemovalCommitted = { events.append("committed \($0)") }
        coordinator.onPlacementsChanged = { events.append("relayout \($0.map(\.photoAssetID).joined(separator: ","))") }
        let before = coordinator.room.photoSlots
        let transformsBefore = coordinator.placements.map(\.transform)

        let outcome = await coordinator.deletePhoto(assetID: "asset_1")

        XCTAssertEqual(outcome, .failed(message: RoomContentEditCoordinator.deletionFailedMessage))
        XCTAssertEqual(coordinator.room.photoSlots, before, "back exactly where it was — same slot, same caption")
        XCTAssertEqual(coordinator.placements.map(\.transform), transformsBefore, "and the layout is what it was")
        XCTAssertEqual(events, [
            "began asset_1",
            "relayout asset_0,asset_2,asset_3,asset_4",
            "reverted asset_1",
            "relayout asset_0,asset_1,asset_2,asset_3,asset_4"
        ], "hidden, relaid out, restored, relaid out — in that order")
        XCTAssertFalse(coordinator.isDeleting(assetID: "asset_1"))
        XCTAssertEqual(coordinator.status, .idle, "a deletion failure does not speak through the reorder status")
    }

    func test_rollback_targetsTheLastServerConfirmedContent() async {
        let (coordinator, spy, _, _) = makeCoordinator(photoCount: 4)
        coordinator.swap(from: 0, to: 3)
        await settle()
        XCTAssertEqual(order(coordinator), ["asset_3", "asset_1", "asset_2", "asset_0"])
        spy.nextDeleteResults = [.failure(IdentityAPIClientError.transport)]

        _ = await coordinator.deletePhoto(assetID: "asset_1")

        XCTAssertEqual(order(coordinator), ["asset_3", "asset_1", "asset_2", "asset_0"], "back to the confirmed order, photograph included")
    }

    // MARK: - Already gone server-side

    func test_photoNotInRoom_isTreatedAsAlreadyDeleted_andConverges() async {
        let (coordinator, spy, museum, _) = makeCoordinator(photoCount: 4)
        spy.nextDeleteResults = [.failure(PhotoAPIError(statusCode: 404, message: nil, code: "photo_not_in_room", assetID: "asset_1"))]
        museum.roomsResult = .success([Room(
            id: "room_1", name: "Trabzon", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: [
                PhotoSlotAssignment(slotIndex: 0, photoAssetID: "asset_3", caption: ""),
                PhotoSlotAssignment(slotIndex: 1, photoAssetID: "asset_0", caption: ""),
                PhotoSlotAssignment(slotIndex: 2, photoAssetID: "asset_2", caption: "")
            ]
        )])
        var committed: [String] = []
        var reverted: [String] = []
        coordinator.onPhotoRemovalCommitted = { committed.append($0) }
        coordinator.onPhotoRemovalReverted = { reverted.append($0) }

        let outcome = await coordinator.deletePhoto(assetID: "asset_1")

        XCTAssertEqual(outcome, .deleted, "already gone is the outcome the owner asked for")
        XCTAssertEqual(committed, ["asset_1"])
        XCTAssertTrue(reverted.isEmpty, "never put back a photograph the server no longer has")
        XCTAssertEqual(museum.fetchRoomCallCount, 1, "the authoritative Room was reloaded")
        XCTAssertEqual(order(coordinator), ["asset_3", "asset_0", "asset_2"])
    }

    // MARK: - Refusals before any request

    func test_delete_aPhotographNotInTheRoom_isRejected() async {
        let (coordinator, spy, _, _) = makeCoordinator()
        let outcome = await coordinator.deletePhoto(assetID: "ghost")
        XCTAssertEqual(outcome, .rejected(message: RoomContentEditCoordinator.photographGoneMessage))
        XCTAssertTrue(spy.deleteCalls.isEmpty)
    }

    func test_delete_twiceWhileInFlight_isRejectedTheSecondTime() async {
        let (coordinator, spy, _, _) = makeCoordinator()
        spy.isDeleteGated = true

        let first = Task { await coordinator.deletePhoto(assetID: "asset_0") }
        await settle()
        let second = await coordinator.deletePhoto(assetID: "asset_0")

        guard case .rejected = second else { return XCTFail("expected a rejection, got \(second)") }
        XCTAssertEqual(spy.deleteCalls.count, 1)
        spy.releaseDeletes()
        _ = await first.value
    }

    func test_deleteAndReplace_ofTheSamePhotograph_areMutuallyExclusive() async {
        let (coordinator, spy, _, replacer) = makeCoordinator()

        replacer.isGated = true
        let replacing = Task { await coordinator.replacePhoto(assetID: "asset_0", with: picked()) }
        await settle()
        let deleteWhileReplacing = await coordinator.deletePhoto(assetID: "asset_0")
        XCTAssertEqual(deleteWhileReplacing, .rejected(message: RoomContentEditCoordinator.replacementInProgressMessage))
        XCTAssertTrue(spy.deleteCalls.isEmpty)
        replacer.release()
        _ = await replacing.value

        spy.isDeleteGated = true
        let deleting = Task { await coordinator.deletePhoto(assetID: "asset_2") }
        await settle()
        let replaceWhileDeleting = await coordinator.replacePhoto(assetID: "asset_2", with: picked("p2"))
        guard case .rejected = replaceWhileDeleting else { return XCTFail("expected a rejection, got \(replaceWhileDeleting)") }
        XCTAssertEqual(replacer.calls.count, 1)
        spy.releaseDeletes()
        _ = await deleting.value
    }

    // MARK: - Independence from the other axes

    func test_delete_duringAnInFlightReorder_neverResurrectsThePhotograph() async {
        let (coordinator, spy, _, _) = makeCoordinator(photoCount: 5)
        var everEmitted: [[String]] = []
        coordinator.onPlacementsChanged = { everEmitted.append($0.map(\.photoAssetID)) }
        spy.isGated = true
        coordinator.swap(from: 0, to: 4)
        await settle()

        let outcome = await coordinator.deletePhoto(assetID: "asset_1")
        XCTAssertEqual(outcome, .deleted)
        XCTAssertEqual(order(coordinator), ["asset_4", "asset_2", "asset_3", "asset_0"], "local order kept, photograph gone")

        spy.releaseAll()
        await settle()

        XCTAssertEqual(coordinator.status, .failedStale, "the reorder that raced the deletion was stale")
        XCTAssertFalse(order(coordinator).contains("asset_1"))
        let emissionsAfterDeletion = everEmitted.drop(while: { $0.contains("asset_1") == false ? false : true }).dropFirst()
        for emission in emissionsAfterDeletion {
            XCTAssertFalse(emission.contains("asset_1"), "the deleted photograph must never come back, even for a frame")
        }
    }

    func test_aStaleReorderConfirmation_cannotResurrectADeletedPhotograph() async {
        let (coordinator, spy, _, _) = makeCoordinator(photoCount: 4)
        spy.isGated = true
        coordinator.swap(from: 0, to: 1)
        await settle()
        spy.nextDeleteResults = [.success(spy.slots(for: ["asset_1", "asset_0", "asset_2"]))]
        spy.nextResults = [.success(spy.slots(for: ["asset_1", "asset_0", "asset_2", "asset_3"]))]

        _ = await coordinator.deletePhoto(assetID: "asset_3")
        XCTAssertEqual(order(coordinator), ["asset_1", "asset_0", "asset_2"])

        spy.releaseAll()
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_1", "asset_0", "asset_2"], "the older confirmation must not bring asset_3 back")
        XCTAssertEqual(coordinator.status, .saved, "the reorder did save; the status must not stay stuck at saving")

        spy.nextResults = [.failure(IdentityAPIClientError.transport)]
        coordinator.swap(from: 0, to: 2)
        await settle()
        XCTAssertEqual(coordinator.status, .failedTransport)
        XCTAssertEqual(order(coordinator), ["asset_1", "asset_0", "asset_2"])
    }

    func test_deleteConfirmation_duringAnInFlightReorder_keepsTheOptimisticOrder() async {
        let (coordinator, spy, _, _) = makeCoordinator(photoCount: 4)
        spy.isGated = true
        coordinator.swap(from: 0, to: 3)
        await settle()

        let outcome = await coordinator.deletePhoto(assetID: "asset_1")

        XCTAssertEqual(outcome, .deleted)
        XCTAssertEqual(order(coordinator), ["asset_3", "asset_2", "asset_0"], "the deletion's answer must not undo the reorder in flight")
        spy.releaseAll()
        await settle()
    }

    func test_aStaleReorderConfirmation_cannotResurrectAReplacedIdentity() async {
        let (coordinator, spy, _, replacer) = makeCoordinator(photoCount: 4)
        spy.isGated = true
        coordinator.swap(from: 0, to: 1)
        await settle()
        spy.nextResults = [.success(spy.slots(for: ["asset_1", "asset_0", "asset_2", "asset_3"]))]

        guard case .replaced(_, let newID) = await coordinator.replacePhoto(assetID: "asset_2", with: picked()) else {
            return XCTFail("expected .replaced")
        }
        _ = replacer
        XCTAssertEqual(order(coordinator), ["asset_1", "asset_0", newID, "asset_3"])

        spy.releaseAll()
        await settle()

        XCTAssertEqual(order(coordinator), ["asset_1", "asset_0", newID, "asset_3"], "the older confirmation must not bring the old identity back")
    }

    func test_deletion_andCaptions_doNotInterfere() async {
        let (coordinator, spy, _, _) = makeCoordinator(photoCount: 4)
        spy.isCaptionGated = true
        let caption = Task { await coordinator.setCaption("typed", forAssetID: "asset_3") }
        await settle()

        let outcome = await coordinator.deletePhoto(assetID: "asset_0")
        XCTAssertEqual(outcome, .deleted)
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_3"), "typed", "the unsaved caption stayed on screen through the deletion")

        spy.releaseCaptions()
        _ = await caption.value
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_3"), "typed")
        XCTAssertEqual(order(coordinator), ["asset_1", "asset_2", "asset_3"])
    }

    func test_deletion_causesNoOtherRequests() async {
        let (coordinator, spy, _, replacer) = makeCoordinator()
        _ = await coordinator.deletePhoto(assetID: "asset_2")
        await settle()

        XCTAssertEqual(spy.deleteCalls, ["asset_2"])
        XCTAssertTrue(spy.orders.isEmpty)
        XCTAssertTrue(spy.captionCalls.isEmpty)
        XCTAssertEqual(spy.uploadInitiations, 0)
        XCTAssertEqual(spy.deliveryTicketRequests, 0)
        XCTAssertTrue(replacer.calls.isEmpty)
    }

    func test_afterADeletion_reorderAndCaptionWorkOnTheCompactedRoom() async {
        let (coordinator, spy, _, _) = makeCoordinator(photoCount: 4, captions: [2: "two"])
        _ = await coordinator.deletePhoto(assetID: "asset_1")

        coordinator.swap(from: 0, to: 2)
        await settle()
        XCTAssertEqual(coordinator.status, .saved)
        XCTAssertEqual(spy.orders.last, ["asset_3", "asset_2", "asset_0"])
        XCTAssertEqual(coordinator.caption(forAssetID: "asset_2"), "two")

        let captioned = await coordinator.setCaption("still here", forAssetID: "asset_2")
        XCTAssertEqual(captioned, .saved)
        let stale = await coordinator.setCaption("x", forAssetID: "asset_1")
        guard case .rejected = stale else { return XCTFail("the deleted photograph must be refused locally, got \(stale)") }
    }

    // MARK: - Teardown

    func test_deactivate_stopsALateDeletionFromTouchingTheScene() async {
        let (coordinator, spy, _, _) = makeCoordinator()
        spy.isDeleteGated = true
        var events = 0
        coordinator.onPlacementsChanged = { _ in events += 1 }
        coordinator.onPhotoRemovalCommitted = { _ in events += 1 }
        coordinator.onPhotoRemovalReverted = { _ in events += 1 }

        let inFlight = Task { await coordinator.deletePhoto(assetID: "asset_0") }
        await settle()
        let eventsBefore = events
        coordinator.deactivate()
        spy.releaseDeletes()
        _ = await inFlight.value

        XCTAssertEqual(events, eventsBefore, "nothing may reach a torn-down scene")
        let after = await coordinator.deletePhoto(assetID: "asset_1")
        guard case .rejected = after else { return XCTFail("a deactivated coordinator must refuse, got \(after)") }
        XCTAssertEqual(spy.deleteCalls.count, 1)
    }
}

@MainActor
final class RoomPhotoLayerRemovalTests: XCTestCase {

    private var arView: ARView!
    private var anchor: AnchorEntity!

    override func setUp() {
        arView = ARView(frame: CGRect(x: 0, y: 0, width: 320, height: 480), cameraMode: .nonAR, automaticallyConfigureSession: false)
        anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        arView = nil
        anchor = nil
    }

    private func placements(_ ids: [String]) -> [ResolvedPhotoPlacement] {
        let room = Room(id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
                        photoSlots: ids.enumerated().map { PhotoSlotAssignment(slotIndex: $0.offset, photoAssetID: $0.element, caption: "cap \($0.element)") })
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: PlaceholderRoomSlotTable.build()) else {
            XCTFail("fixture table must resolve")
            return []
        }
        return placements
    }

    private func ids(_ count: Int) -> [String] { (0..<count).map { "a\($0)" } }

    private func image(width: Int, height: Int) -> DecodedPhotoImage {
        try! PhotoTextureDecoder.decode(PhotoTextureDecoderTests.jpeg(width: width, height: height), maxLongEdge: 256)
    }

    func test_beginRemoval_hidesThePhotograph_andRevertRestoresItWithItsTexture() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(ids(4)))
        await layer.apply(image(width: 300, height: 200), forAsset: "a1", generation: gen)
        let boundsBefore = layer.planeBounds(forAsset: "a1")!

        XCTAssertTrue(layer.beginRemoval(assetID: "a1"))

        XCTAssertEqual(layer.root.children.count, 3, "the photograph is off the wall")
        XCTAssertEqual(layer.mountedAssetIDs, ["a0", "a2", "a3"])
        XCTAssertEqual(layer.pendingRemovalAssetIDs, ["a1"])
        XCTAssertNil(layer.slotIndex(forAsset: "a1"), "not placed, not a drop target")
        XCTAssertFalse(layer.isTextured(assetID: "a1"))

        layer.revertRemoval(assetID: "a1")

        XCTAssertEqual(layer.root.children.count, 4)
        XCTAssertEqual(layer.mountedAssetIDs, ["a0", "a1", "a2", "a3"])
        XCTAssertTrue(layer.pendingRemovalAssetIDs.isEmpty)
        XCTAssertTrue(layer.isTextured(assetID: "a1"), "back with its texture")
        let boundsAfter = layer.planeBounds(forAsset: "a1")!
        XCTAssertEqual(boundsAfter.x, boundsBefore.x, accuracy: 0.0001)
        XCTAssertEqual(boundsAfter.y, boundsBefore.y, accuracy: 0.0001)
    }

    func test_whileHidden_theRemainingPhotographsRelayoutToTheCompactedPositions() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(ids(5)))
        layer.beginRemoval(assetID: "a1")

        let compacted = placements(["a0", "a2", "a3", "a4"])
        layer.relayout(compacted)

        XCTAssertEqual(layer.slotIndex(forAsset: "a2"), 1)
        XCTAssertEqual(layer.slotIndex(forAsset: "a4"), 3)
        XCTAssertEqual(layer.transform(forAsset: "a4")?.position, compacted[3].transform.position)
        XCTAssertEqual(layer.mountedSlotIndices, Set(0..<4), "contiguous, no gap")
    }

    func test_aTextureArrivingWhileHidden_isKeptForARollback() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(ids(3)))
        layer.beginRemoval(assetID: "a2")

        await layer.apply(image(width: 200, height: 300), forAsset: "a2", generation: gen)
        XCTAssertFalse(layer.isTextured(assetID: "a2"), "hidden photographs do not count as shown")

        layer.revertRemoval(assetID: "a2")

        XCTAssertTrue(layer.isTextured(assetID: "a2"), "restored with the texture that arrived meanwhile")
        let bounds = layer.planeBounds(forAsset: "a2")!
        XCTAssertLessThan(bounds.x, bounds.y, "and at its real aspect")
    }

    func test_commitRemoval_dropsThePhotograph_andLateEventsForItAreIgnored() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(ids(3)))
        layer.beginRemoval(assetID: "a0")

        layer.commitRemoval(assetID: "a0")

        XCTAssertTrue(layer.pendingRemovalAssetIDs.isEmpty)
        XCTAssertEqual(layer.mountedAssetIDs, ["a1", "a2"])
        XCTAssertEqual(layer.root.children.count, 2)
        await layer.apply(image(width: 300, height: 200), forAsset: "a0", generation: gen)
        layer.setStoredDimensions(pixelWidth: 1, pixelHeight: 9, forAsset: "a0", generation: gen)
        XCTAssertEqual(layer.root.children.count, 2, "a late event must not resurrect a plane")
        XCTAssertTrue(layer.texturedAssetIDs.isEmpty)
    }

    func test_beginRemoval_ofALiftedPhotograph_endsTheInteractionFeedback() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(ids(3)))
        layer.setLifted(assetID: "a1")
        XCTAssertEqual(layer.liftedAssetIDForTesting, "a1")

        layer.beginRemoval(assetID: "a1")

        XCTAssertNil(layer.liftedAssetIDForTesting)
        XCTAssertNil(layer.targetAssetIDForTesting)
    }

    func test_beginRemoval_ofAnUnknownPhotograph_isRefused() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(ids(2)))
        XCTAssertFalse(layer.beginRemoval(assetID: "ghost"))
        XCTAssertEqual(layer.root.children.count, 2)
    }

    func test_tearDown_dropsHiddenPhotographsToo() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(ids(3)))
        await layer.apply(image(width: 300, height: 200), forAsset: "a0", generation: gen)
        layer.beginRemoval(assetID: "a0")

        layer.tearDown()

        XCTAssertTrue(layer.pendingRemovalAssetIDs.isEmpty)
        XCTAssertEqual(layer.root.children.count, 0)
        layer.revertRemoval(assetID: "a0")
        XCTAssertEqual(layer.root.children.count, 0, "nothing survives teardown")
    }
}

@MainActor
final class RoomDeletionRuntimeTests: XCTestCase {

    private func waitUntil(_ timeout: TimeInterval = 20, _ condition: @escaping @MainActor () -> Bool) async {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition() && Date() < deadline {
            try? await Task.sleep(nanoseconds: 50_000_000)
        }
    }

    private func ownerController(photoCount: Int, photoService: (any RoomPhotoServicing)? = nil) -> RealityKitSceneViewController {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .owner, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: photoService ?? fixture.photoService, roomService: fixture.roomService,
            photoReplacer: fixture.photoReplacer
        )
        let controller = RealityKitSceneViewController(content: content, photoPicker: FakePhotoPicker(photos: []))
        controller.loadViewIfNeeded()
        return controller
    }

    func test_delete_removesThePhotograph_compactsTheRest_andKeepsTheirCaptions() async {
        let controller = ownerController(photoCount: 5)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 5 }
        controller.enterEditMode()
        let layer = controller.photoLayer!
        let generationBefore = layer.generation
        XCTAssertNotNil(controller.captionLayer?.caption(forAsset: "fixture-asset-1"), "slot 1 starts captioned")

        controller.deletePhoto(forAsset: "fixture-asset-1")
        await waitUntil { controller.contentCoordinator?.isDeleting(assetID: "fixture-asset-1") == false && layer.mountedAssetIDs.count == 4 }

        XCTAssertFalse(layer.mountedAssetIDs.contains("fixture-asset-1"))
        XCTAssertEqual(layer.mountedSlotIndices, Set(0..<4), "compacted — contiguous slots, no gap")
        XCTAssertEqual(layer.slotIndex(forAsset: "fixture-asset-2"), 1, "the next photograph moved up")
        XCTAssertEqual(layer.texturedAssetIDs.count, 4, "no texture was lost or re-downloaded")
        XCTAssertEqual(layer.generation, generationBefore, "no remount")
        XCTAssertTrue(layer.pendingRemovalAssetIDs.isEmpty)
        XCTAssertEqual(controller.texturedPhotoCount, 4)
        XCTAssertEqual(controller.contentCoordinator?.room.photoSlots.count, 4)
        XCTAssertNil(controller.captionLayer?.caption(forAsset: "fixture-asset-1"))
        XCTAssertEqual(controller.captionLayer?.caption(forAsset: "fixture-asset-0"), RoomRenderingVerificationFixture.seedCaption(forSlot: 0))
        XCTAssertNil(controller.editNoticeForTesting)
        controller.viewDidDisappear(false)
    }

    func test_delete_failure_restoresThePhotograph_withAnInlineNotice() async {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 4)!
        let failing = FailingDeleteService(inner: fixture.photoService!)
        let controller = ownerController(photoCount: 4, photoService: failing)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 4 }
        controller.enterEditMode()
        let layer = controller.photoLayer!
        let positionBefore = layer.transform(forAsset: "fixture-asset-2")?.position

        controller.deletePhoto(forAsset: "fixture-asset-2")
        await waitUntil { controller.editNoticeForTesting == RoomContentEditCoordinator.deletionFailedMessage }

        XCTAssertEqual(layer.mountedAssetIDs.count, 4, "the photograph is back")
        XCTAssertTrue(layer.isTextured(assetID: "fixture-asset-2"), "with its texture")
        XCTAssertEqual(layer.transform(forAsset: "fixture-asset-2")?.position, positionBefore, "exactly where it was")
        XCTAssertEqual(layer.mountedSlotIndices, Set(0..<4))
        XCTAssertTrue(layer.pendingRemovalAssetIDs.isEmpty)
        XCTAssertEqual(controller.texturedPhotoCount, 4)
        controller.viewDidDisappear(false)
    }

    func test_deletingTheOnlyPhotograph_leavesAnEmptyRoom() async {
        let controller = ownerController(photoCount: 1)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 1 }
        controller.enterEditMode()

        controller.deletePhoto(forAsset: "fixture-asset-0")
        await waitUntil { controller.photoLayer?.mountedAssetIDs.isEmpty == true && controller.contentCoordinator?.isDeleting(assetID: "fixture-asset-0") == false }

        XCTAssertEqual(controller.photoLayer?.root.children.count, 0)
        XCTAssertEqual(controller.contentCoordinator?.room.photoSlots.count, 0)
        XCTAssertEqual(controller.texturedPhotoCount, 0)
        XCTAssertNil(controller.captionLayer?.caption(forAsset: "fixture-asset-0"))
        controller.viewDidDisappear(false)
    }

    func test_visitor_hasNoDeletionPath() {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 2)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .visitor, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: fixture.photoService, roomService: fixture.roomService
        )
        let controller = RealityKitSceneViewController(content: content, photoPicker: FakePhotoPicker(photos: []))
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertNil(controller.contentCoordinator, "no coordinator, no deletion, no tap")
        controller.deletePhoto(forAsset: "fixture-asset-0")
        XCTAssertEqual(controller.photoLayer?.mountedAssetIDs.count, 2, "a visitor's Room is untouched")
        controller.viewDidDisappear(false)
    }
}

final class FailingDeleteService: RoomPhotoServicing, @unchecked Sendable {
    private let inner: any RoomPhotoServicing

    init(inner: any RoomPhotoServicing) {
        self.inner = inner
    }

    func deletePhoto(accessToken: String, roomID: String, photoAssetID: String) async throws -> [PhotoSlotAssignment] {
        throw IdentityAPIClientError.transport
    }

    func initiateUpload(accessToken: String, declaration: PhotoUploadDeclaration) async throws -> PhotoUploadTicket {
        try await inner.initiateUpload(accessToken: accessToken, declaration: declaration)
    }
    func assignPhotos(accessToken: String, roomID: String, assetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        try await inner.assignPhotos(accessToken: accessToken, roomID: roomID, assetIDs: assetIDs)
    }
    func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] {
        try await inner.fetchPhotoURLs(accessToken: accessToken, roomID: roomID)
    }
    func reorderPhotos(accessToken: String, roomID: String, orderedAssetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        try await inner.reorderPhotos(accessToken: accessToken, roomID: roomID, orderedAssetIDs: orderedAssetIDs)
    }
    func setPhotoCaption(accessToken: String, roomID: String, photoAssetID: String, caption: String) async throws -> [PhotoSlotAssignment] {
        try await inner.setPhotoCaption(accessToken: accessToken, roomID: roomID, photoAssetID: photoAssetID, caption: caption)
    }
    func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, replacementAssetID: String) async throws -> [PhotoSlotAssignment] {
        try await inner.replacePhoto(accessToken: accessToken, roomID: roomID, photoAssetID: photoAssetID, replacementAssetID: replacementAssetID)
    }
}
