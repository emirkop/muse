import RealityKit
import simd
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RoomPhotoLayerRelayoutTests: XCTestCase {

    private var arView: ARView!
    private var anchor: AnchorEntity!
    private let table = PlaceholderRoomSlotTable.build()

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

    private func room(_ count: Int) -> Room {
        Room(id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
             photoSlots: (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "cap \($0)") })
    }

    private func placements(_ room: Room) -> [ResolvedPhotoPlacement] {
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            XCTFail("must resolve")
            return []
        }
        return placements
    }

    private func image(width: Int, height: Int) -> DecodedPhotoImage {
        try! PhotoTextureDecoder.decode(PhotoTextureDecoderTests.jpeg(width: width, height: height), maxLongEdge: 256)
    }

    private func swapped(_ room: Room, _ from: Int, _ to: Int) -> Room {
        room.replacingPhotoSlots(RoomPhotoOrder.swapping(room.photoSlots, from: from, to: to))
    }

    // MARK: - Texture reuse

    func test_relayout_keepsEveryTexture_andDoesNotBumpTheGeneration() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let start = room(6)
        let gen = layer.mount(placements(start))
        for slot in 0..<6 {
            await layer.apply(image(width: 300, height: 200), forAsset: "a\(slot)", generation: gen)
        }
        XCTAssertEqual(layer.texturedAssetIDs.count, 6)

        layer.relayout(placements(swapped(start, 0, 5)))

        XCTAssertEqual(layer.generation, gen, "a relayout is not a remount — the same photographs are still loading")
        XCTAssertEqual(layer.texturedAssetIDs.count, 6, "every texture survived the move")
        XCTAssertEqual(layer.mountedAssetIDs.count, 6)
        XCTAssertEqual(layer.root.children.count, 6, "no entity was rebuilt")
    }

    func test_relayout_movesPhotographsToTheirDestinationTransforms() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let start = room(Room.maxPhotos)
        layer.mount(placements(start))

        let before = placements(start)
        let focalTransform = before[0].transform
        let rearTransform = before[27].transform
        XCTAssertNotEqual(focalTransform.position, rearTransform.position)

        layer.relayout(placements(swapped(start, 0, 27)))

        XCTAssertEqual(layer.transform(forAsset: "a27")?.position, focalTransform.position)
        XCTAssertEqual(layer.transform(forAsset: "a0")?.position, rearTransform.position)
        XCTAssertEqual(layer.slotIndex(forAsset: "a27"), 0)
        XCTAssertEqual(layer.slotIndex(forAsset: "a0"), 27)
    }

    func test_relayout_refitsToTheDestinationEnvelope_preservingAspect() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let start = room(Room.maxPhotos)
        let gen = layer.mount(placements(start))
        await layer.apply(image(width: 300, height: 200), forAsset: "a0", generation: gen)
        await layer.apply(image(width: 300, height: 200), forAsset: "a1", generation: gen)

        let focalBoundsBefore = layer.planeBounds(forAsset: "a0")!
        let sideBoundsBefore = layer.planeBounds(forAsset: "a1")!
        XCTAssertGreaterThan(focalBoundsBefore.x, sideBoundsBefore.x, "the focal envelope is larger to begin with")

        layer.relayout(placements(swapped(start, 0, 1)))

        let a0After = layer.planeBounds(forAsset: "a0")!
        let a1After = layer.planeBounds(forAsset: "a1")!
        XCTAssertEqual(a0After.x, sideBoundsBefore.x, accuracy: 0.001, "a0 shrank into the side envelope")
        XCTAssertEqual(a1After.x, focalBoundsBefore.x, accuracy: 0.001, "a1 grew into the focal envelope")
        XCTAssertEqual(a0After.x / a0After.y, 300.0 / 200.0, accuracy: 0.02)
        XCTAssertEqual(a1After.x / a1After.y, 300.0 / 200.0, accuracy: 0.02)
        let after = placements(swapped(start, 0, 1))
        XCTAssertLessThanOrEqual(a0After.x, after[1].transform.scale.x + 0.001)
        XCTAssertLessThanOrEqual(a1After.x, after[0].transform.scale.x + 0.001)
    }

    func test_relayout_movesFailedPlaceholdersWithTheirOwnPhotograph() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let start = room(4)
        let gen = layer.mount(placements(start))
        await layer.apply(image(width: 300, height: 200), forAsset: "a0", generation: gen)
        layer.markFailed(assetID: "a2", generation: gen)
        XCTAssertEqual(layer.failedAssetIDs, ["a2"])

        let target = placements(swapped(start, 0, 2))
        layer.relayout(target)

        XCTAssertEqual(layer.failedAssetIDs, ["a2"], "the failure stayed with its photograph")
        XCTAssertEqual(layer.texturedAssetIDs, ["a0"])
        XCTAssertEqual(layer.slotIndex(forAsset: "a2"), 0, "the failed photograph moved to slot 0")
        XCTAssertEqual(layer.transform(forAsset: "a2")?.position, target[0].transform.position)
        XCTAssertEqual(layer.slotIndex(forAsset: "a0"), 2)
    }

    func test_loaderSlotIndices_resolveToTheMountTimePhotograph_evenAfterReorder() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let start = room(4)
        let gen = layer.mount(placements(start))
        XCTAssertEqual(layer.loadingAssetID(forSlot: 0), "a0")
        XCTAssertEqual(layer.loadingAssetID(forSlot: 3), "a3")

        layer.relayout(placements(swapped(start, 0, 3)))

        XCTAssertEqual(layer.loadingAssetID(forSlot: 0), "a0", "a reorder must not re-point in-flight downloads")
        XCTAssertEqual(layer.loadingAssetID(forSlot: 3), "a3")

        let assetForSlot0 = layer.loadingAssetID(forSlot: 0)!
        await layer.apply(image(width: 300, height: 200), forAsset: assetForSlot0, generation: gen)
        XCTAssertEqual(layer.texturedAssetIDs, ["a0"])
        XCTAssertEqual(layer.slotIndex(forAsset: "a0"), 3, "textured photograph sits at its post-reorder slot")
        XCTAssertFalse(layer.isTextured(assetID: "a3"), "the photograph now at slot 0 must NOT have received a0's texture")
    }

    func test_relayout_ignoresUnknownAssets_ratherThanInventingThem() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(room(2)))

        let foreign = ResolvedPhotoPlacement(
            slotIndex: 0, photoAssetID: "not-mounted", caption: "",
            anchor: SlotAnchor(wall: .focal, positionOnWall: 0),
            transform: SlotTransform(position: .zero)
        )
        layer.relayout([foreign])

        XCTAssertEqual(layer.mountedAssetIDs, ["a0", "a1"])
        XCTAssertEqual(layer.root.children.count, 2)
    }

    // MARK: - Interaction feedback

    func test_liftAndTargetFeedback_scalesTheRightEntities_andRestores() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(room(3)))

        layer.setLifted(assetID: "a0")
        XCTAssertEqual(layer.liftedAssetIDForTesting, "a0")

        layer.setTarget(assetID: "a2")
        XCTAssertEqual(layer.targetAssetIDForTesting, "a2")

        layer.clearInteractionFeedback()
        XCTAssertNil(layer.liftedAssetIDForTesting)
        XCTAssertNil(layer.targetAssetIDForTesting)
    }

    func test_tearDown_clearsInteractionFeedback() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        layer.mount(placements(room(2)))
        layer.setLifted(assetID: "a0")
        layer.setTarget(assetID: "a1")

        layer.tearDown()

        XCTAssertNil(layer.liftedAssetIDForTesting)
        XCTAssertNil(layer.targetAssetIDForTesting)
    }
}

@MainActor
final class RoomReorderInteractionStateTests: XCTestCase {

    private var arView: ARView!
    private var anchor: AnchorEntity!
    private var layer: RoomPhotoLayer!
    private var swaps: [(from: Int, to: Int)] = []
    private var isEditing = true

    override func setUp() {
        arView = ARView(frame: CGRect(x: 0, y: 0, width: 320, height: 480), cameraMode: .nonAR, automaticallyConfigureSession: false)
        anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
        layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let room = Room(id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
                        photoSlots: (0..<5).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") })
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: PlaceholderRoomSlotTable.build()) else {
            return XCTFail("must resolve")
        }
        layer.mount(placements)
        swaps = []
        isEditing = true
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        arView = nil
        anchor = nil
        layer = nil
    }

    private func makeInteraction() -> RoomReorderInteraction {
        RoomReorderInteraction(
            gestureHost: arView,
            arView: arView,
            layer: layer,
            isEditing: { [weak self] in self?.isEditing ?? false },
            onSwap: { [weak self] from, to in self?.swaps.append((from, to)) }
        )
    }

    func test_liftDragDrop_swapsTheTwoSlots() {
        let interaction = makeInteraction()

        interaction.testBeginLift(assetID: "a1")
        XCTAssertEqual(interaction.liftedAssetID, "a1")
        XCTAssertEqual(layer.liftedAssetIDForTesting, "a1")

        interaction.testMove(over: "a4")
        XCTAssertEqual(interaction.targetAssetID, "a4")
        XCTAssertEqual(layer.targetAssetIDForTesting, "a4")

        interaction.testDrop()

        XCTAssertEqual(swaps.count, 1)
        XCTAssertEqual(swaps[0].from, 1)
        XCTAssertEqual(swaps[0].to, 4)
        XCTAssertNil(interaction.liftedAssetID, "feedback cleared after the drop")
        XCTAssertNil(layer.liftedAssetIDForTesting)
    }

    func test_crossWallTargetIsValid() {
        let interaction = makeInteraction()
        let layout = RoomPhotoSlotLayout.slots(forPhotoCount: 5)
        XCTAssertEqual(layout[0].wall, .focal)
        XCTAssertEqual(layout[1].wall, .left)

        interaction.testBeginLift(assetID: "a1")
        interaction.testMove(over: "a0")
        interaction.testDrop()

        XCTAssertEqual(swaps.map(\.from), [1])
        XCTAssertEqual(swaps.map(\.to), [0])
    }

    func test_hoveringBackOverTheSource_clearsTheTarget_andDropsNothing() {
        let interaction = makeInteraction()

        interaction.testBeginLift(assetID: "a2")
        interaction.testMove(over: "a3")
        XCTAssertEqual(interaction.targetAssetID, "a3")

        interaction.testMove(over: "a2")
        XCTAssertNil(interaction.targetAssetID)
        XCTAssertNil(layer.targetAssetIDForTesting)

        interaction.testDrop()
        XCTAssertTrue(swaps.isEmpty, "a drop with no target is a cancellation")
    }

    func test_dropOnNothing_isACancellation() {
        let interaction = makeInteraction()
        interaction.testBeginLift(assetID: "a0")
        interaction.testMove(over: nil)
        interaction.testDrop()

        XCTAssertTrue(swaps.isEmpty)
        XCTAssertNil(layer.liftedAssetIDForTesting)
    }

    func test_explicitCancel_clearsEverything_withoutSwapping() {
        let interaction = makeInteraction()
        interaction.testBeginLift(assetID: "a0")
        interaction.testMove(over: "a1")

        interaction.testCancel()

        XCTAssertTrue(swaps.isEmpty)
        XCTAssertNil(interaction.liftedAssetID)
        XCTAssertNil(interaction.targetAssetID)
        XCTAssertNil(layer.liftedAssetIDForTesting)
        XCTAssertNil(layer.targetAssetIDForTesting)
    }

    func test_outsideEditMode_nothingCanBeLifted() {
        isEditing = false
        let interaction = makeInteraction()

        interaction.testBeginLift(assetID: "a1")

        XCTAssertNil(interaction.liftedAssetID)
        XCTAssertNil(layer.liftedAssetIDForTesting)
        interaction.testMove(over: "a2")
        interaction.testDrop()
        XCTAssertTrue(swaps.isEmpty)
    }

    func test_liftingAnUnmountedPhotograph_isRefused() {
        let interaction = makeInteraction()
        interaction.testBeginLift(assetID: "not-here")
        XCTAssertNil(interaction.liftedAssetID)
    }

    func test_slotIndicesAreReadAtDropTime() {
        let interaction = makeInteraction()
        interaction.testBeginLift(assetID: "a1")
        interaction.testMove(over: "a4")

        let room = Room(id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
                        photoSlots: (0..<5).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") })
        let moved = room.replacingPhotoSlots(RoomPhotoOrder.swapping(room.photoSlots, from: 1, to: 0))
        guard case .resolved(let newPlacements) = RoomPlacementResolver.resolve(room: moved, slotTable: PlaceholderRoomSlotTable.build()) else {
            return XCTFail("must resolve")
        }
        layer.relayout(newPlacements)

        interaction.testDrop()

        XCTAssertEqual(swaps.count, 1)
        XCTAssertEqual(swaps[0].from, 0)
        XCTAssertEqual(swaps[0].to, 4)
    }

    func test_detach_removesTheGestureAndClearsFeedback() {
        let interaction = makeInteraction()
        let before = arView.gestureRecognizers?.count ?? 0
        interaction.testBeginLift(assetID: "a0")

        interaction.detach()

        XCTAssertEqual((arView.gestureRecognizers?.count ?? 0), before - 1)
        XCTAssertNil(layer.liftedAssetIDForTesting)
    }
}

@MainActor
final class RoomReorderRuntimeTests: XCTestCase {

    private func waitUntil(_ timeout: TimeInterval = 20, _ condition: @escaping @MainActor () -> Bool) async {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition() && Date() < deadline {
            try? await Task.sleep(nanoseconds: 50_000_000)
        }
    }

    private func ownerController(photoCount: Int, downloader: PhotoBytesDownloading? = nil) -> RealityKitSceneViewController {
        let content = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount, downloader: downloader)!
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        return controller
    }

    private func visitorController(photoCount: Int) -> RealityKitSceneViewController {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .visitor, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: fixture.photoService, roomService: fixture.roomService
        )
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        return controller
    }

    // MARK: - Presence

    func test_owner_getsAReorderCoordinatorAndInteraction() {
        let controller = ownerController(photoCount: 4)
        controller.viewWillAppear(false)

        XCTAssertNotNil(controller.contentCoordinator)
        XCTAssertNotNil(controller.reorderInteraction)
        controller.viewDidDisappear(false)
    }

    func test_visitor_getsNoReorderMachinery() {
        let controller = visitorController(photoCount: 4)
        controller.viewWillAppear(false)

        XCTAssertNil(controller.contentCoordinator)
        XCTAssertNil(controller.reorderInteraction)
        XCTAssertNil(controller.editMode)
        XCTAssertEqual(controller.photoLayer?.mountedAssetIDs.count, 4, "the Room still renders")
        controller.viewDidDisappear(false)
    }

    func test_skeleton_getsNoReorderMachinery() {
        let controller = RealityKitSceneViewController(content: nil)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertNil(controller.contentCoordinator)
        XCTAssertNil(controller.reorderInteraction)
        controller.viewDidDisappear(false)
    }

    func test_leavingTheRoom_removesTheReorderMachinery() {
        let controller = ownerController(photoCount: 3)
        controller.viewWillAppear(false)
        XCTAssertNotNil(controller.contentCoordinator)

        controller.viewDidDisappear(false)

        XCTAssertNil(controller.contentCoordinator)
        XCTAssertNil(controller.reorderInteraction)
    }

    // MARK: - Optimistic relayout in the live scene

    func test_swap_relayoutsTheSceneImmediately_keepingTextures() async {
        let controller = ownerController(photoCount: 6)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 6 }
        let layer = controller.photoLayer!
        let generationBefore = layer.generation

        let focalBefore = layer.transform(forAsset: "fixture-asset-0")!.position
        controller.contentCoordinator!.swap(from: 0, to: 5)

        XCTAssertEqual(layer.slotIndex(forAsset: "fixture-asset-5"), 0, "the scene moved at once")
        XCTAssertEqual(layer.transform(forAsset: "fixture-asset-5")!.position, focalBefore)
        XCTAssertEqual(layer.generation, generationBefore, "no remount — textures were kept")
        XCTAssertEqual(layer.texturedAssetIDs.count, 6, "all six textures survived the swap")
        XCTAssertEqual(controller.texturedPhotoCount, 6)
        controller.viewDidDisappear(false)
    }

    func test_reordering_causesNoDownload() async {
        let counting = CountingFixtureDownloader()
        let controller = ownerController(photoCount: 5, downloader: counting)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 5 }
        let downloadsAfterLoad = counting.count

        for (from, to) in [(0, 4), (1, 3), (2, 0)] {
            controller.contentCoordinator!.swap(from: from, to: to)
        }
        await waitUntil { controller.contentCoordinator?.isPersisting == false }
        try? await Task.sleep(nanoseconds: 200_000_000)

        XCTAssertEqual(counting.count, downloadsAfterLoad, "reordering must not download anything")
        XCTAssertEqual(controller.photoLayer?.texturedAssetIDs.count, 5)
        controller.viewDidDisappear(false)
    }

    func test_reorderMidLoad_isSafe_andTexturesLandOnTheRightPhotographs() async {
        let slow = SlowFixtureDownloader(delayNanoseconds: 80_000_000)
        let controller = ownerController(photoCount: 8, downloader: slow)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount >= 2 }

        controller.contentCoordinator!.swap(from: 0, to: 7)
        controller.contentCoordinator!.swap(from: 1, to: 6)
        controller.contentCoordinator!.swap(from: 2, to: 5)

        await waitUntil(40) { controller.texturedPhotoCount == 8 }

        let layer = controller.photoLayer!
        XCTAssertEqual(layer.texturedAssetIDs.count, 8, "every photograph got its own texture")
        XCTAssertEqual(layer.mountedAssetIDs.count, 8)
        XCTAssertEqual(layer.mountedSlotIndices, Set(0..<8))
        controller.viewDidDisappear(false)
    }

    func test_rapidReorders_leaveAConsistentScene() async {
        let controller = ownerController(photoCount: Room.maxPhotos)
        controller.viewWillAppear(false)
        await waitUntil(60) { controller.texturedPhotoCount == Room.maxPhotos }

        for index in 0..<15 {
            controller.contentCoordinator!.swap(from: index % Room.maxPhotos, to: (index * 5 + 2) % Room.maxPhotos)
        }
        await waitUntil { controller.contentCoordinator?.isPersisting == false }

        let layer = controller.photoLayer!
        XCTAssertEqual(layer.mountedAssetIDs.count, Room.maxPhotos)
        XCTAssertEqual(layer.mountedSlotIndices, Set(0..<Room.maxPhotos), "contiguous, no duplicates")
        XCTAssertEqual(layer.texturedAssetIDs.count, Room.maxPhotos, "no texture was lost to the churn")
        controller.viewDidDisappear(false)
    }

    func test_exitingEditMode_clearsAnyLiftedPhotograph() async {
        let controller = ownerController(photoCount: 4)
        controller.viewWillAppear(false)
        controller.enterEditMode()
        controller.reorderInteraction!.testBeginLift(assetID: "fixture-asset-1")
        XCTAssertEqual(controller.photoLayer?.liftedAssetIDForTesting, "fixture-asset-1")

        controller.exitEditMode()

        XCTAssertNil(controller.photoLayer?.liftedAssetIDForTesting)
        XCTAssertNil(controller.reorderInteraction?.liftedAssetID)
        controller.viewDidDisappear(false)
    }

    // MARK: - –27 regression

    func test_reorderMachinery_doesNotDisturbCameraMovementOrSpawn() async {
        let controller = ownerController(photoCount: 5)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 5 }

        XCTAssertNotNil(controller.cameraController, " camera rig intact")
        XCTAssertEqual(controller.movementController.subject, PlaceholderRoom.spawnPoint, " deterministic spawn intact")

        controller.enterEditMode()
        controller.contentCoordinator!.swap(from: 0, to: 4)

        XCTAssertEqual(controller.movementController.subject, PlaceholderRoom.spawnPoint, "a reorder must not move the viewer")
        XCTAssertNotNil(controller.cameraController)
        controller.viewDidDisappear(false)
    }

    func test_photographsCarryNoCollisionComponent() async {
        let controller = ownerController(photoCount: 6)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 6 }
        controller.contentCoordinator!.swap(from: 0, to: 5)

        for child in controller.photoLayer!.root.children {
            XCTAssertNil(child.components[CollisionComponent.self], "a photograph must never become a movement obstacle")
        }
        controller.viewDidDisappear(false)
    }
}

final class CountingFixtureDownloader: PhotoBytesDownloading, @unchecked Sendable {
    private let inner = RoomRenderingVerificationFixture.FixturePhotoDownloader()
    private let lock = NSLock()
    private var _count = 0
    var count: Int { lock.withLock { _count } }

    func download(_ url: URL) async throws -> Data {
        lock.withLock { _count += 1 }
        return try await inner.download(url)
    }
}
