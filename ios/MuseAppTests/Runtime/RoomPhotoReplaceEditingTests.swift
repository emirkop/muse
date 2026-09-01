import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RoomPhotoReplaceEditingTests: XCTestCase {

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
        photoCount: Int = 4,
        captions: [Int: String] = [:],
        withReplacer: Bool = true
    ) -> (RoomContentEditCoordinator, SpyReorderService, FakeReplacer) {
        let room = room(photoCount, captions: captions)
        let spy = SpyReorderService(room: room)
        let replacer = FakeReplacer(server: spy)
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            fatalError("fixture must resolve")
        }
        let coordinator = RoomContentEditCoordinator(
            room: room, slotTable: table, placements: placements,
            photoService: spy, roomService: FakeMuseumService(), accessToken: "tok",
            photoReplacer: withReplacer ? replacer : nil
        )
        return (coordinator, spy, replacer)
    }

    private func picked(_ id: String = "picked_1", normalized: Bool = true) -> PickedPhoto {
        guard normalized else { return PickedPhoto(id: id, assetIdentifier: nil, loadState: .failed) }
        let file = NormalizedPhotoFile(
            fileURL: URL(fileURLWithPath: "/tmp/\(id).jpg"), contentType: "image/jpeg", byteSize: 64,
            pixelWidth: 1200, pixelHeight: 800, sha256Hex: String(repeating: "a", count: 64)
        )
        return PickedPhoto(id: id, assetIdentifier: nil, loadState: .ready(thumbnail: Data([1]), file: file))
    }

    private func settle() async {
        for _ in 0..<12 { await Task.yield() }
    }

    // MARK: - Confirmation adopts the identity

    func test_replace_adoptsTheNewIdentityOnConfirmation_keepingSlotAndCaption() async {
        let (coordinator, _, replacer) = makeCoordinator(captions: [1: "kept"])
        let transformsBefore = coordinator.placements.map(\.transform)

        let outcome = await coordinator.replacePhoto(assetID: "asset_1", with: picked())

        guard case .replaced(_, let newID) = outcome else { return XCTFail("expected .replaced, got \(outcome)") }
        XCTAssertEqual(newID, replacer.replacementID(for: "picked_1"))
        XCTAssertEqual(RoomPhotoOrder.assetIDs(coordinator.room.photoSlots), ["asset_0", newID, "asset_2", "asset_3"])
        XCTAssertEqual(coordinator.room.photoSlots[1].slotIndex, 1, "position preserved")
        XCTAssertEqual(coordinator.caption(forAssetID: newID), "kept", "the caption travelled to the new identity")
        XCTAssertNil(coordinator.caption(forAssetID: "asset_1"), "the old identity is gone")
        XCTAssertEqual(coordinator.placements.map(\.transform), transformsBefore, "no photograph moved")
        XCTAssertEqual(coordinator.placements[1].photoAssetID, newID)
        XCTAssertEqual(coordinator.placements[1].caption, "kept")
        XCTAssertEqual(replacer.calls.map(\.assetID), ["asset_1"])
        XCTAssertEqual(replacer.calls.map(\.roomID), ["room_1"])
    }

    func test_replace_reKeysTheRendererBeforeTheRelayout() async {
        let (coordinator, _, replacer) = makeCoordinator()
        var events: [String] = []
        coordinator.onPhotoAssetReplaced = { previous, replacement in events.append("rekey \(previous)->\(replacement)") }
        coordinator.onPlacementsChanged = { placements in events.append("relayout \(placements.map(\.photoAssetID)[2])") }

        _ = await coordinator.replacePhoto(assetID: "asset_2", with: picked())

        let newID = replacer.replacementID(for: "picked_1")
        XCTAssertEqual(events, ["rekey asset_2->\(newID)", "relayout \(newID)"])
    }

    func test_replace_localContentIsUnchangedUntilTheServerConfirms() async {
        let (coordinator, _, replacer) = makeCoordinator()
        replacer.isGated = true
        var emissions = 0
        coordinator.onPlacementsChanged = { _ in emissions += 1 }

        let task = Task { await coordinator.replacePhoto(assetID: "asset_1", with: picked()) }
        await settle()

        XCTAssertEqual(RoomPhotoOrder.assetIDs(coordinator.room.photoSlots), ["asset_0", "asset_1", "asset_2", "asset_3"], "nothing adopted yet")
        XCTAssertTrue(coordinator.isReplacing(assetID: "asset_1"))
        XCTAssertEqual(emissions, 0)

        replacer.release()
        let outcome = await task.value
        guard case .replaced = outcome else { return XCTFail("expected .replaced, got \(outcome)") }
        XCTAssertFalse(coordinator.isReplacing(assetID: "asset_1"))
        XCTAssertEqual(emissions, 1, "one relayout, on confirmation")
    }

    func test_replace_theServersCaptionForTheNewIdentityWins() async {
        let (coordinator, _, replacer) = makeCoordinator(captions: [0: "mine"])
        replacer.nextOutcome = .replaced(
            photoSlots: [
                PhotoSlotAssignment(slotIndex: 0, photoAssetID: "server-new", caption: "server's"),
                PhotoSlotAssignment(slotIndex: 1, photoAssetID: "asset_1", caption: ""),
                PhotoSlotAssignment(slotIndex: 2, photoAssetID: "asset_2", caption: ""),
                PhotoSlotAssignment(slotIndex: 3, photoAssetID: "asset_3", caption: "")
            ],
            replacementAssetID: "server-new"
        )

        _ = await coordinator.replacePhoto(assetID: "asset_0", with: picked())

        XCTAssertEqual(coordinator.caption(forAssetID: "server-new"), "server's")
    }

    // MARK: - Failure applies nothing

    func test_replace_failure_leavesContentExactlyAsItWas_andReportsIt() async {
        let (coordinator, _, replacer) = makeCoordinator(captions: [1: "kept"])
        replacer.nextOutcome = .failed(.transferFailed)
        var rekeys = 0
        var emissions = 0
        coordinator.onPhotoAssetReplaced = { _, _ in rekeys += 1 }
        coordinator.onPlacementsChanged = { _ in emissions += 1 }
        let before = coordinator.room.photoSlots

        let outcome = await coordinator.replacePhoto(assetID: "asset_1", with: picked())

        XCTAssertEqual(outcome, .failed(.transferFailed))
        XCTAssertEqual(coordinator.room.photoSlots, before, "nothing to roll back: nothing was applied")
        XCTAssertEqual(rekeys, 0)
        XCTAssertEqual(emissions, 0)
        XCTAssertEqual(coordinator.status, .idle, "a replacement failure does not speak through the reorder status")
        XCTAssertFalse(coordinator.isReplacing(assetID: "asset_1"), "the photograph is replaceable again")
    }

    func test_replace_serverRefusalNamingThePhotograph_isReported() async {
        let (coordinator, _, replacer) = makeCoordinator()
        replacer.nextOutcome = .failed(.rejectedAtCommit(code: "photo_not_in_room"))

        let outcome = await coordinator.replacePhoto(assetID: "asset_0", with: picked())

        XCTAssertEqual(outcome, .failed(.rejectedAtCommit(code: "photo_not_in_room")))
        XCTAssertEqual(
            RoomContentEditCoordinator.replacementFailureMessage(for: .rejectedAtCommit(code: "photo_not_in_room")),
            RoomContentEditCoordinator.photographGoneMessage
        )
    }

    // MARK: - Refusals before any upload

    func test_replace_aPhotographNotInTheRoom_isRejectedWithoutUploading() async {
        let (coordinator, _, replacer) = makeCoordinator()
        let outcome = await coordinator.replacePhoto(assetID: "ghost", with: picked())

        XCTAssertEqual(outcome, .rejected(message: RoomContentEditCoordinator.photographGoneMessage))
        XCTAssertTrue(replacer.calls.isEmpty)
    }

    func test_replace_anUnnormalizedPhotograph_failsWithoutUploading() async {
        let (coordinator, _, replacer) = makeCoordinator()
        let outcome = await coordinator.replacePhoto(assetID: "asset_0", with: picked(normalized: false))

        XCTAssertEqual(outcome, .failed(.notNormalized))
        XCTAssertTrue(replacer.calls.isEmpty)
    }

    func test_replace_aSecondReplacementOnTheSamePhotographWhileInFlight_isRejected() async {
        let (coordinator, _, replacer) = makeCoordinator()
        replacer.isGated = true

        let first = Task { await coordinator.replacePhoto(assetID: "asset_0", with: picked("p1")) }
        await settle()
        let second = await coordinator.replacePhoto(assetID: "asset_0", with: picked("p2"))

        XCTAssertEqual(second, .rejected(message: RoomContentEditCoordinator.replacementInProgressMessage))
        XCTAssertEqual(replacer.calls.count, 1, "the second attempt never uploaded")

        let other = Task { await coordinator.replacePhoto(assetID: "asset_2", with: picked("p3")) }
        await settle()
        XCTAssertEqual(replacer.calls.count, 2)

        replacer.release()
        _ = await first.value
        _ = await other.value
    }

    func test_withoutAReplacer_theAffordanceIsAbsent_andReplaceIsRejected() async {
        let (coordinator, _, replacer) = makeCoordinator(withReplacer: false)
        XCTAssertFalse(coordinator.isReplacementAvailable)

        let outcome = await coordinator.replacePhoto(assetID: "asset_0", with: picked())

        XCTAssertEqual(outcome, .rejected(message: RoomContentEditCoordinator.replacementUnavailableMessage))
        XCTAssertTrue(replacer.calls.isEmpty)
    }

    // MARK: - Independence from the other axes

    func test_replace_duringAnInFlightReorder_keepsTheLocalOrder() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 4)
        spy.isGated = true
        coordinator.swap(from: 0, to: 3)
        await settle()

        let outcome = await coordinator.replacePhoto(assetID: "asset_1", with: picked())

        guard case .replaced(_, let newID) = outcome else { return XCTFail("expected .replaced") }
        XCTAssertEqual(RoomPhotoOrder.assetIDs(coordinator.room.photoSlots), ["asset_3", newID, "asset_2", "asset_0"], "local order kept, identity adopted")

        spy.releaseAll()
        await settle()
        XCTAssertEqual(coordinator.status, .failedStale)
        XCTAssertFalse(RoomPhotoOrder.assetIDs(coordinator.room.photoSlots).contains("asset_1"), "the replaced identity never comes back")
        XCTAssertTrue(RoomPhotoOrder.assetIDs(coordinator.room.photoSlots).contains(newID))
    }

    func test_afterAReplacement_captionAndReorderUseTheNewIdentity() async {
        let (coordinator, spy, _) = makeCoordinator(photoCount: 3, captions: [1: "kept"])
        guard case .replaced(_, let newID) = await coordinator.replacePhoto(assetID: "asset_1", with: picked()) else {
            return XCTFail("expected .replaced")
        }

        let captionOutcome = await coordinator.setCaption("new words", forAssetID: newID)
        XCTAssertEqual(captionOutcome, .saved)
        XCTAssertEqual(spy.captionCalls.last?.assetID, newID)
        XCTAssertEqual(coordinator.caption(forAssetID: newID), "new words")

        coordinator.swap(from: 1, to: 2)
        await settle()
        XCTAssertEqual(coordinator.status, .saved)
        XCTAssertEqual(spy.orders.last, ["asset_0", "asset_2", newID])
        XCTAssertEqual(coordinator.caption(forAssetID: newID), "new words", "the caption followed the new identity through the swap")

        let stale = await coordinator.setCaption("x", forAssetID: "asset_1")
        guard case .rejected = stale else { return XCTFail("the old identity must be refused locally, got \(stale)") }
    }

    func test_replacement_causesNoReorderCaptionOrDirectServiceCalls() async {
        let (coordinator, spy, _) = makeCoordinator()
        _ = await coordinator.replacePhoto(assetID: "asset_0", with: picked())
        await settle()

        XCTAssertTrue(spy.orders.isEmpty)
        XCTAssertTrue(spy.captionCalls.isEmpty)
        XCTAssertEqual(spy.replaceCalls, 0, "the coordinator does not call the service; the replacer does")
        XCTAssertEqual(spy.uploadInitiations, 0)
        XCTAssertEqual(spy.deliveryTicketRequests, 0, "a replacement causes no download")
    }

    // MARK: - Teardown

    func test_deactivate_stopsALateReplacementFromTouchingTheScene() async {
        let (coordinator, _, replacer) = makeCoordinator()
        replacer.isGated = true
        var emissions = 0
        var rekeys = 0
        coordinator.onPlacementsChanged = { _ in emissions += 1 }
        coordinator.onPhotoAssetReplaced = { _, _ in rekeys += 1 }
        let before = coordinator.room.photoSlots

        let inFlight = Task { await coordinator.replacePhoto(assetID: "asset_0", with: picked()) }
        await settle()
        coordinator.deactivate()
        replacer.release()
        _ = await inFlight.value

        XCTAssertEqual(emissions, 0)
        XCTAssertEqual(rekeys, 0)
        XCTAssertEqual(coordinator.room.photoSlots, before, "a torn-down scene adopts nothing")
        let after = await coordinator.replacePhoto(assetID: "asset_1", with: picked("p2"))
        guard case .rejected = after else { return XCTFail("a deactivated coordinator must refuse, got \(after)") }
        XCTAssertEqual(replacer.calls.count, 1)
    }
}

@MainActor
final class FakeReplacer: RoomPhotoReplacing {
    struct Call: Equatable {
        let roomID: String
        let assetID: String
        let pickedID: String
    }

    private(set) var calls: [Call] = []
    var nextOutcome: PhotoReplacementOutcome?
    var isGated = false
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private let server: SpyReorderService

    init(server: SpyReorderService) {
        self.server = server
    }

    func replacementID(for pickedID: String) -> String { "new-\(pickedID)" }

    func release() {
        isGated = false
        let pending = waiters
        waiters.removeAll()
        for waiter in pending { waiter.resume() }
    }

    func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, with photo: PickedPhoto) async -> PhotoReplacementOutcome {
        calls.append(Call(roomID: roomID, assetID: photoAssetID, pickedID: photo.id))
        if isGated {
            await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                waiters.append(continuation)
            }
        }
        if let next = nextOutcome {
            nextOutcome = nil
            return next
        }
        let newID = replacementID(for: photo.id)
        return .replaced(photoSlots: server.applyReplacement(of: photoAssetID, with: newID), replacementAssetID: newID)
    }
}

@MainActor
final class RoomPhotoLayerReplacementTests: XCTestCase {

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

    private func placements(_ count: Int) -> [ResolvedPhotoPlacement] {
        let room = Room(id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
                        photoSlots: (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "cap \($0)") })
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: PlaceholderRoomSlotTable.build()) else {
            XCTFail("fixture table must resolve \(count)")
            return []
        }
        return placements
    }

    private func image(width: Int, height: Int) -> DecodedPhotoImage {
        try! PhotoTextureDecoder.decode(PhotoTextureDecoderTests.jpeg(width: width, height: height), maxLongEdge: 256)
    }

    private func material(_ layer: RoomPhotoLayer, _ assetID: String) -> PhysicallyBasedMaterial? {
        (layer.root.children.first { $0.name == "photo-\(assetID)" } as? ModelEntity)?.model?.materials.first as? PhysicallyBasedMaterial
    }

    func test_preview_showsTheReplacementOnThePlane_andRevertRestoresExactly() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(3))
        await layer.apply(image(width: 300, height: 200), forAsset: "a1", generation: gen)
        let boundsBefore = layer.planeBounds(forAsset: "a1")!
        XCTAssertGreaterThan(boundsBefore.x, boundsBefore.y)

        let previewed = await layer.beginReplacementPreview(image(width: 200, height: 300), forAsset: "a1")

        XCTAssertTrue(previewed)
        XCTAssertEqual(layer.previewingReplacementAssetIDs, ["a1"])
        let boundsDuring = layer.planeBounds(forAsset: "a1")!
        XCTAssertLessThan(boundsDuring.x, boundsDuring.y, "the plane shows the replacement's aspect")
        XCTAssertTrue(layer.isTextured(assetID: "a1"))
        XCTAssertEqual(layer.root.children.count, 3, "no entity was added or rebuilt")

        layer.revertReplacementPreview(forAsset: "a1")

        XCTAssertTrue(layer.previewingReplacementAssetIDs.isEmpty)
        let boundsAfter = layer.planeBounds(forAsset: "a1")!
        XCTAssertEqual(boundsAfter.x, boundsBefore.x, accuracy: 0.0001)
        XCTAssertEqual(boundsAfter.y, boundsBefore.y, accuracy: 0.0001)
        XCTAssertTrue(layer.isTextured(assetID: "a1"), "the previous texture is back")
        XCTAssertNotNil(material(layer, "a1")?.baseColor.texture)
    }

    func test_revert_onAnUntexturedPhotograph_restoresThePlaceholder() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(2))
        XCTAssertFalse(layer.isTextured(assetID: "a0"))

        _ = await layer.beginReplacementPreview(image(width: 300, height: 200), forAsset: "a0")
        XCTAssertTrue(layer.isTextured(assetID: "a0"))
        layer.revertReplacementPreview(forAsset: "a0")

        XCTAssertFalse(layer.isTextured(assetID: "a0"))
        XCTAssertNil(material(layer, "a0")?.baseColor.texture, "back to the neutral placeholder material")
    }

    func test_previewOnAnUnmountedPhotograph_isRefused() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(2))
        let applied = await layer.beginReplacementPreview(image(width: 300, height: 200), forAsset: "ghost")
        XCTAssertFalse(applied)
        XCTAssertTrue(layer.previewingReplacementAssetIDs.isEmpty)
    }

    func test_commit_reKeysTheEntity_keepingItsTexture_andDropsLateEventsForTheOldIdentity() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(3))
        _ = await layer.beginReplacementPreview(image(width: 200, height: 300), forAsset: "a2")
        let entityBefore = layer.root.children.first { $0.name == "photo-a2" }

        layer.commitReplacement(from: "a2", to: "new")

        XCTAssertEqual(layer.mountedAssetIDs, ["a0", "a1", "new"])
        XCTAssertTrue(layer.isTextured(assetID: "new"), "the preview pixels are now the photograph — nothing to download")
        XCTAssertEqual(layer.slotIndex(forAsset: "new"), 2)
        XCTAssertTrue(layer.root.children.first { $0.name == "photo-new" } === entityBefore, "same entity, new key")
        XCTAssertNil(layer.root.children.first { $0.name == "photo-a2" })
        XCTAssertTrue(layer.previewingReplacementAssetIDs.isEmpty, "a committed preview is no longer revertable")
        XCTAssertEqual(layer.root.children.count, 3)

        XCTAssertEqual(layer.loadingAssetID(forSlot: 2), "a2", "the mount-time map still names the old download")
        let boundsBefore = layer.planeBounds(forAsset: "new")!
        await layer.apply(image(width: 300, height: 100), forAsset: "a2", generation: gen)
        let boundsAfter = layer.planeBounds(forAsset: "new")!
        XCTAssertEqual(boundsAfter.x, boundsBefore.x, accuracy: 0.0001, "the late event was dropped")
        XCTAssertEqual(boundsAfter.y, boundsBefore.y, accuracy: 0.0001)
    }

    func test_afterCommit_relayoutUnderTheNewKeyMovesTheEntity() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let before = placements(4)
        layer.mount(before)
        layer.commitReplacement(from: "a1", to: "new")

        let room = Room(id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
                        photoSlots: RoomPhotoOrder.swapping(
                            RoomPhotoReplacement.replacing("a1", with: "new", in: (0..<4).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") }),
                            from: 1, to: 3))
        guard case .resolved(let after) = RoomPlacementResolver.resolve(room: room, slotTable: PlaceholderRoomSlotTable.build()) else {
            return XCTFail("must resolve")
        }
        layer.relayout(after)

        XCTAssertEqual(layer.slotIndex(forAsset: "new"), 3)
        XCTAssertEqual(layer.transform(forAsset: "new")?.position, before[3].transform.position)
        XCTAssertEqual(layer.slotIndex(forAsset: "a3"), 1)
    }

    func test_commit_withTheSameIdentity_orAnUnknownOne_changesNothing() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(2))

        layer.commitReplacement(from: "a0", to: "a0")
        layer.commitReplacement(from: "ghost", to: "new")

        XCTAssertEqual(layer.mountedAssetIDs, ["a0", "a1"])
    }

    func test_tearDown_dropsPreviewState() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(2))
        _ = await layer.beginReplacementPreview(image(width: 300, height: 200), forAsset: "a0")

        layer.tearDown()

        XCTAssertTrue(layer.previewingReplacementAssetIDs.isEmpty)
        XCTAssertEqual(layer.root.children.count, 0)
    }
}

@MainActor
final class RoomReplacementRuntimeTests: XCTestCase {

    private var spoolDir: URL!

    override func setUpWithError() throws {
        spoolDir = FileManager.default.temporaryDirectory.appendingPathComponent("replace-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: spoolDir, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: spoolDir)
    }

    private func waitUntil(_ timeout: TimeInterval = 20, _ condition: @escaping @MainActor () -> Bool) async {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition() && Date() < deadline {
            try? await Task.sleep(nanoseconds: 50_000_000)
        }
    }

    private func pickedPhoto(_ id: String = "picked-replacement", normalized: Bool = true) throws -> PickedPhoto {
        guard normalized else { return PickedPhoto(id: id, assetIdentifier: nil, loadState: .failed) }
        let data = PhotoTextureDecoderTests.jpeg(width: 600, height: 400)
        let url = spoolDir.appendingPathComponent("\(id).jpg")
        try data.write(to: url)
        let file = NormalizedPhotoFile(fileURL: url, contentType: "image/jpeg", byteSize: data.count, pixelWidth: 600, pixelHeight: 400, sha256Hex: String(repeating: "b", count: 64))
        return PickedPhoto(id: id, assetIdentifier: nil, loadState: .ready(thumbnail: Data([1]), file: file))
    }

    private func ownerController(photoCount: Int, picker: FakePhotoPicker, replacer: (any RoomPhotoReplacing)? = nil) -> RealityKitSceneViewController {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .owner, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: fixture.photoService, roomService: fixture.roomService,
            photoReplacer: replacer ?? fixture.photoReplacer
        )
        let controller = RealityKitSceneViewController(content: content, photoPicker: picker)
        controller.loadViewIfNeeded()
        return controller
    }

    func test_replace_previewsThenConfirms_reKeyingThePhotographAndKeepingItsCaption() async throws {
        let photo = try pickedPhoto()
        let picker = FakePhotoPicker(photos: [photo])
        let controller = ownerController(photoCount: 4, picker: picker)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 4 }
        controller.enterEditMode()
        let layer = controller.photoLayer!
        let generationBefore = layer.generation
        let portraitBefore = layer.planeBounds(forAsset: "fixture-asset-1")!
        XCTAssertLessThan(portraitBefore.x, portraitBefore.y, "fixture slot 1 is portrait")

        controller.beginReplacement(forAsset: "fixture-asset-1")
        let newID = "fixture-replacement-\(photo.id)"
        await waitUntil { layer.mountedAssetIDs.contains(newID) && controller.editNoticeForTesting == nil }

        XCTAssertEqual(picker.requestedLimits, [1], "the picker is scoped to exactly one photograph")
        XCTAssertTrue(layer.mountedAssetIDs.contains(newID))
        XCTAssertFalse(layer.mountedAssetIDs.contains("fixture-asset-1"))
        XCTAssertEqual(layer.slotIndex(forAsset: newID), 1, "same slot")
        XCTAssertTrue(layer.isTextured(assetID: newID), "the preview pixels are the photograph — no download")
        let bounds = layer.planeBounds(forAsset: newID)!
        XCTAssertGreaterThan(bounds.x, bounds.y, "the new (landscape) photograph is what shows")
        XCTAssertEqual(layer.generation, generationBefore, "no remount")
        XCTAssertEqual(controller.texturedPhotoCount, 4)
        XCTAssertEqual(controller.contentCoordinator?.caption(forAssetID: newID), RoomRenderingVerificationFixture.seedCaption(forSlot: 1))
        XCTAssertEqual(controller.captionLayer?.caption(forAsset: newID), RoomRenderingVerificationFixture.seedCaption(forSlot: 1))
        XCTAssertNil(controller.captionLayer?.caption(forAsset: "fixture-asset-1"))
        XCTAssertNil(controller.editNoticeForTesting)
        controller.viewDidDisappear(false)
    }

    func test_replace_failure_revertsToThePreviousPhotograph_withAnInlineNotice() async throws {
        let photo = try pickedPhoto()
        let picker = FakePhotoPicker(photos: [photo])
        let gated = GatedFailingReplacer()
        let controller = ownerController(photoCount: 4, picker: picker, replacer: gated)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 4 }
        controller.enterEditMode()
        let layer = controller.photoLayer!
        let before = layer.planeBounds(forAsset: "fixture-asset-1")!

        controller.beginReplacement(forAsset: "fixture-asset-1")
        await waitUntil { layer.previewingReplacementAssetIDs.contains("fixture-asset-1") }

        let during = layer.planeBounds(forAsset: "fixture-asset-1")!
        XCTAssertGreaterThan(during.x, during.y, "optimistic preview of the landscape replacement")
        XCTAssertEqual(controller.editNoticeForTesting, RealityKitSceneViewController.replacingMessage)

        gated.fail(with: .transferFailed)
        await waitUntil { controller.editNoticeForTesting == RoomContentEditCoordinator.replacementFailedMessage }

        let after = layer.planeBounds(forAsset: "fixture-asset-1")!
        XCTAssertEqual(after.x, before.x, accuracy: 0.0001, "the previous photograph is back, exactly")
        XCTAssertEqual(after.y, before.y, accuracy: 0.0001)
        XCTAssertTrue(layer.isTextured(assetID: "fixture-asset-1"))
        XCTAssertTrue(layer.previewingReplacementAssetIDs.isEmpty)
        XCTAssertEqual(layer.mountedAssetIDs.count, 4, "no entity was lost")
        XCTAssertEqual(controller.contentCoordinator?.caption(forAssetID: "fixture-asset-1"), RoomRenderingVerificationFixture.seedCaption(forSlot: 1), "content untouched")
        controller.viewDidDisappear(false)
    }

    func test_replace_aPhotographThatFailedToLoad_isReportedAndChangesNothing() async throws {
        let picker = FakePhotoPicker(photos: [try pickedPhoto(normalized: false)])
        let controller = ownerController(photoCount: 3, picker: picker)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 3 }
        controller.enterEditMode()
        let mountedBefore = controller.photoLayer!.mountedAssetIDs

        controller.beginReplacement(forAsset: "fixture-asset-0")
        await waitUntil { controller.editNoticeForTesting != nil }

        XCTAssertEqual(controller.editNoticeForTesting, RoomContentEditCoordinator.replacementCouldNotLoadMessage)
        XCTAssertEqual(controller.photoLayer?.mountedAssetIDs, mountedBefore)
        controller.viewDidDisappear(false)
    }

    func test_cancellingThePicker_changesNothing() async throws {
        let picker = FakePhotoPicker(photos: [])
        let controller = ownerController(photoCount: 3, picker: picker)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 3 }
        controller.enterEditMode()
        let mountedBefore = controller.photoLayer!.mountedAssetIDs

        controller.beginReplacement(forAsset: "fixture-asset-0")
        await waitUntil { picker.requestedLimits.count == 1 }
        try? await Task.sleep(nanoseconds: 100_000_000)

        XCTAssertEqual(controller.photoLayer?.mountedAssetIDs, mountedBefore)
        XCTAssertNil(controller.editNoticeForTesting)
        XCTAssertTrue(controller.photoLayer!.previewingReplacementAssetIDs.isEmpty)
        controller.viewDidDisappear(false)
    }

    func test_withoutAReplacer_replaceIsStructurallyAbsent() async {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 2)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .owner, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: fixture.photoService, roomService: fixture.roomService
        )
        XCTAssertFalse(content.supportsPhotoReplacement)
        XCTAssertTrue(content.supportsOwnerEditing)
        let picker = FakePhotoPicker(photos: [])
        let controller = RealityKitSceneViewController(content: content, photoPicker: picker)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        controller.enterEditMode()

        XCTAssertEqual(controller.contentCoordinator?.isReplacementAvailable, false)
        XCTAssertNotNil(controller.photoTapInteraction, "captions still need the tap")
        controller.beginReplacement(forAsset: "fixture-asset-0")
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertTrue(picker.requestedLimits.isEmpty, "the picker must never be reached without a pipeline to upload through")
        controller.viewDidDisappear(false)
    }

    func test_visitor_hasNoTapInteractionAtAll() {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 2)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .visitor, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: fixture.photoService, roomService: fixture.roomService,
            photoReplacer: fixture.photoReplacer
        )
        XCTAssertFalse(content.supportsPhotoReplacement, "a visitor gets no replacement even with a pipeline present")
        let controller = RealityKitSceneViewController(content: content, photoPicker: FakePhotoPicker(photos: []))
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        XCTAssertNil(controller.photoTapInteraction)
        XCTAssertNil(controller.contentCoordinator)
        controller.viewDidDisappear(false)
    }
}

@MainActor
final class FakePhotoPicker: PhotoPicking {
    private(set) var requestedLimits: [Int] = []
    private let photos: [PickedPhoto]

    init(photos: [PickedPhoto]) {
        self.photos = photos
    }

    func pickPhotos(limit: Int, presentingFrom viewController: UIViewController) async -> [PickedPhoto] {
        requestedLimits.append(limit)
        return photos
    }
}

final class GatedFailingReplacer: RoomPhotoReplacing, @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<PhotoUploadFailure, Never>?
    private var pendingFailure: PhotoUploadFailure?

    func fail(with failure: PhotoUploadFailure) {
        let waiter: CheckedContinuation<PhotoUploadFailure, Never>? = lock.withLock {
            if let continuation { self.continuation = nil; return continuation }
            pendingFailure = failure
            return nil
        }
        waiter?.resume(returning: failure)
    }

    func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, with photo: PickedPhoto) async -> PhotoReplacementOutcome {
        let failure = await withCheckedContinuation { (continuation: CheckedContinuation<PhotoUploadFailure, Never>) in
            let immediate: PhotoUploadFailure? = lock.withLock {
                if let pendingFailure { self.pendingFailure = nil; return pendingFailure }
                self.continuation = continuation
                return nil
            }
            if let immediate { continuation.resume(returning: immediate) }
        }
        return .failed(failure)
    }
}
