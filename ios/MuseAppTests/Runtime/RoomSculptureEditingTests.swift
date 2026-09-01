import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RoomSculptureEditCoordinatorTests: XCTestCase {

    private func makeCoordinator(
        sculptures: [SculptureInstance] = []
    ) -> (RoomSculptureEditCoordinator, FakeMuseumService) {
        let service = FakeMuseumService()
        service.sculptures = RoomSculptures.sorted(sculptures)
        let coordinator = RoomSculptureEditCoordinator(
            roomID: "room_1", sculptures: sculptures, service: service, accessToken: "tok"
        )
        return (coordinator, service)
    }

    private func settle() async {
        for _ in 0..<12 { await Task.yield() }
    }

    // MARK: - Adding

    func test_add_placesAtTheLowestFreeSlot_andConverges() async {
        let (coordinator, service) = makeCoordinator()
        var emitted: [[Int]] = []
        coordinator.onSculpturesChanged = { emitted.append($0.map(\.slotIndex)) }

        let outcome = await coordinator.add(catalogID: "sculpture_a")

        guard case .applied(let sculptures) = outcome else { return XCTFail("expected .applied, got \(outcome)") }
        XCTAssertEqual(sculptures.map(\.slotIndex), [0])
        XCTAssertEqual(coordinator.sculptures.map(\.catalogID), ["sculpture_a"])
        XCTAssertEqual(service.addSculptureCalls, ["sculpture_a"])
        XCTAssertEqual(emitted, [[0]], "one optimistic emission; the server agreed so no second")
    }

    func test_add_reusesAFreedSlot() async {
        let (coordinator, _) = makeCoordinator(sculptures: [
            SculptureInstance(slotIndex: 0, catalogID: "a"),
            SculptureInstance(slotIndex: 2, catalogID: "c")
        ])

        _ = await coordinator.add(catalogID: "new")

        XCTAssertEqual(coordinator.sculptures.map(\.slotIndex), [0, 1, 2])
        XCTAssertEqual(coordinator.sculptures.map(\.catalogID), ["a", "new", "c"])
    }

    func test_add_toAFullRoom_isRejectedLocallyWithoutSending() async {
        let (coordinator, service) = makeCoordinator(sculptures: (0..<3).map {
            SculptureInstance(slotIndex: $0, catalogID: "s\($0)")
        })
        XCTAssertFalse(coordinator.hasCapacity)
        XCTAssertEqual(coordinator.remainingCapacity, 0)

        let outcome = await coordinator.add(catalogID: "fourth")

        XCTAssertEqual(outcome, .rejected(message: RoomSculptureEditCoordinator.fullMessage))
        XCTAssertTrue(service.addSculptureCalls.isEmpty, "a full Room must not reach the server")
        XCTAssertEqual(coordinator.sculptures.count, 3)
    }

    func test_add_serverRefusal_restoresExactly_andReportsTheReason() async {
        let (coordinator, service) = makeCoordinator(sculptures: [SculptureInstance(slotIndex: 0, catalogID: "a")])
        service.addSculptureError = PhotoAPIError(statusCode: 400, message: nil, code: "unknown_sculpture", assetID: nil)
        let before = coordinator.sculptures

        let outcome = await coordinator.add(catalogID: "ghost")

        XCTAssertEqual(outcome, .failed(message: RoomSculptureEditCoordinator.unknownSculptureMessage))
        XCTAssertEqual(coordinator.sculptures, before, "the Room is exactly as it was")
    }

    func test_add_transportFailure_restoresExactly() async {
        let (coordinator, service) = makeCoordinator()
        service.addSculptureError = IdentityAPIClientError.transport
        var emitted: [[Int]] = []
        coordinator.onSculpturesChanged = { emitted.append($0.map(\.slotIndex)) }

        let outcome = await coordinator.add(catalogID: "a")

        XCTAssertEqual(outcome, .failed(message: RoomSculptureEditCoordinator.addFailedMessage))
        XCTAssertTrue(coordinator.sculptures.isEmpty)
        XCTAssertEqual(emitted, [[0], []], "shown optimistically, then taken back")
    }

    func test_add_serverCapacityRefusal_isSurfacedAsTheFullMessage() async {
        let (coordinator, service) = makeCoordinator()
        service.addSculptureError = PhotoAPIError(statusCode: 409, message: nil, code: "sculpture_capacity_reached", assetID: nil)

        let outcome = await coordinator.add(catalogID: "a")

        XCTAssertEqual(outcome, .failed(message: RoomSculptureEditCoordinator.fullMessage))
        XCTAssertTrue(coordinator.sculptures.isEmpty)
    }

    func test_add_anEmptyCatalogID_isRejectedWithoutSending() async {
        let (coordinator, service) = makeCoordinator()
        let outcome = await coordinator.add(catalogID: "")
        XCTAssertEqual(outcome, .rejected(message: RoomSculptureEditCoordinator.unknownSculptureMessage))
        XCTAssertTrue(service.addSculptureCalls.isEmpty)
    }

    // MARK: - Removing

    func test_remove_leavesTheSlotEmpty_andNothingElseMoves() async {
        let (coordinator, service) = makeCoordinator(sculptures: (0..<3).map {
            SculptureInstance(slotIndex: $0, catalogID: "s\($0)")
        })

        let outcome = await coordinator.remove(slotIndex: 1)

        guard case .applied = outcome else { return XCTFail("expected .applied, got \(outcome)") }
        XCTAssertEqual(coordinator.sculptures.map(\.slotIndex), [0, 2], "no compaction")
        XCTAssertEqual(coordinator.sculptures.map(\.catalogID), ["s0", "s2"], "nothing relocated")
        XCTAssertEqual(service.removeSculptureCalls, [1])
        XCTAssertTrue(coordinator.hasCapacity)
    }

    func test_remove_isVisibleBeforeTheServerAnswers() async {
        let (coordinator, service) = makeCoordinator(sculptures: [SculptureInstance(slotIndex: 0, catalogID: "a")])
        var emitted: [[Int]] = []
        coordinator.onSculpturesChanged = { emitted.append($0.map(\.slotIndex)) }
        service.removeSculptureError = nil

        _ = await coordinator.remove(slotIndex: 0)

        XCTAssertEqual(emitted.first, [], "removed locally before the answer")
        XCTAssertTrue(coordinator.sculptures.isEmpty)
    }

    func test_remove_failure_putsTheSculptureBackExactly() async {
        let placed = [
            SculptureInstance(slotIndex: 0, catalogID: "a"),
            SculptureInstance(slotIndex: 2, catalogID: "c")
        ]
        let (coordinator, service) = makeCoordinator(sculptures: placed)
        service.removeSculptureError = IdentityAPIClientError.transport

        let outcome = await coordinator.remove(slotIndex: 2)

        XCTAssertEqual(outcome, .failed(message: RoomSculptureEditCoordinator.removeFailedMessage))
        XCTAssertEqual(coordinator.sculptures, placed, "back in the same slot, same catalog id")
    }

    func test_remove_alreadyGoneServerSide_commits() async {
        let (coordinator, service) = makeCoordinator(sculptures: [SculptureInstance(slotIndex: 1, catalogID: "b")])
        service.removeSculptureError = PhotoAPIError(statusCode: 404, message: nil, code: "sculpture_not_in_room", assetID: nil)

        let outcome = await coordinator.remove(slotIndex: 1)

        guard case .applied = outcome else { return XCTFail("expected .applied, got \(outcome)") }
        XCTAssertTrue(coordinator.sculptures.isEmpty, "never put back something the server no longer has")
    }

    func test_remove_anEmptyOrInvalidSlot_isRejectedWithoutSending() async {
        let (coordinator, service) = makeCoordinator(sculptures: [SculptureInstance(slotIndex: 0, catalogID: "a")])

        for slot in [1, 2, -1, Room.maxSculptures] {
            let outcome = await coordinator.remove(slotIndex: slot)
            XCTAssertEqual(outcome, .rejected(message: RoomSculptureEditCoordinator.emptySlotMessage), "slot \(slot)")
        }
        XCTAssertTrue(service.removeSculptureCalls.isEmpty)
    }

    // MARK: - Serialisation and teardown

    func test_aSecondActionWhileOneIsInFlight_isRejected() async {
        let (coordinator, service) = makeCoordinator()
        let gate = AsyncGate()
        service.addSculptureError = nil
        service.beforeAddSculpture = { await gate.wait() }

        let first = Task { await coordinator.add(catalogID: "a") }
        await settle()
        XCTAssertTrue(coordinator.isEditing)

        let second = await coordinator.add(catalogID: "b")
        XCTAssertEqual(second, .rejected(message: RoomSculptureEditCoordinator.busyMessage))

        await gate.open()
        _ = await first.value
        XCTAssertEqual(service.addSculptureCalls, ["a"])
        XCTAssertFalse(coordinator.isEditing)
    }

    func test_deactivate_stopsMutationAndLateEmissions() async {
        let (coordinator, service) = makeCoordinator()
        let gate = AsyncGate()
        service.beforeAddSculpture = { await gate.wait() }
        var emissions = 0
        coordinator.onSculpturesChanged = { _ in emissions += 1 }

        let inFlight = Task { await coordinator.add(catalogID: "a") }
        await settle()
        let before = emissions

        coordinator.deactivate()
        await gate.open()
        _ = await inFlight.value

        XCTAssertEqual(emissions, before, "no emission may reach a torn-down scene")
        let after = await coordinator.add(catalogID: "b")
        XCTAssertEqual(after, .rejected(message: RoomSculptureEditCoordinator.unavailableMessage))
        XCTAssertEqual(service.addSculptureCalls, ["a"])
    }

    func test_capacityCopy_reflectsTheRemainingSlots() {
        let (empty, _) = makeCoordinator()
        XCTAssertEqual(empty.remainingCapacity, 3)
        let (two, _) = makeCoordinator(sculptures: (0..<2).map { SculptureInstance(slotIndex: $0, catalogID: "s") })
        XCTAssertEqual(two.capacityMessage, "You can add 1 more sculpture.")
        let (full, _) = makeCoordinator(sculptures: (0..<3).map { SculptureInstance(slotIndex: $0, catalogID: "s") })
        XCTAssertEqual(full.capacityMessage, RoomSculptureEditCoordinator.fullMessage)
    }
}

actor AsyncGate {
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var isOpen = false

    func wait() async {
        if isOpen { return }
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            waiters.append(continuation)
        }
    }

    func open() {
        isOpen = true
        let pending = waiters
        waiters.removeAll()
        for waiter in pending { waiter.resume() }
    }
}

@MainActor
final class RoomSculptureLayerTests: XCTestCase {

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

    private func makeLayer(models: any SculptureModelProviding = RoomRenderingVerificationFixture.FixtureSculptureModelProvider()) -> RoomSculptureLayer {
        let layer = RoomSculptureLayer(slotTable: PlaceholderRoomSlotTable.build(), models: models)
        anchor.addChild(layer.root)
        return layer
    }

    private func fixtureSculpture(slot: Int) -> SculptureInstance {
        SculptureInstance(slotIndex: slot, catalogID: RoomRenderingVerificationFixture.sculptureCatalogID)
    }

    func test_mountsEachSculptureAtItsAuthoredTransform() async {
        let layer = makeLayer()
        let table = PlaceholderRoomSlotTable.build()

        await layer.apply([fixtureSculpture(slot: 0), fixtureSculpture(slot: 2)])

        XCTAssertEqual(layer.mountedSlotIndices, [0, 2])
        XCTAssertEqual(layer.root.children.count, 2)
        for slot in [0, 2] {
            XCTAssertEqual(layer.position(atSlot: slot), table.sculptureTransforms[slot]?.position, "slot \(slot)")
        }
        XCTAssertTrue(layer.unrenderableSlotIndices.isEmpty)
    }

    func test_withTheProductionModelProvider_nothingIsMounted_andItIsReported() async {
        let layer = makeLayer(models: UnavailableSculptureModelProvider())

        await layer.apply([SculptureInstance(slotIndex: 0, catalogID: "sculpture_real")])

        XCTAssertTrue(layer.mountedSlotIndices.isEmpty, "a sculpture with no model must not be drawn")
        XCTAssertEqual(layer.root.children.count, 0)
        XCTAssertEqual(layer.unrenderableSlotIndices, [0], "and the runtime must say so rather than hide it")
    }

    func test_aSlotWithNoAuthoredTransform_isReportedUnrenderable() async {
        let bare = RoomSculptureLayer(
            slotTable: RoomVariantSlotTable(variantID: "v", photoTransforms: [:], sculptureTransforms: [:]),
            models: RoomRenderingVerificationFixture.FixtureSculptureModelProvider()
        )
        anchor.addChild(bare.root)

        await bare.apply([fixtureSculpture(slot: 0)])

        XCTAssertTrue(bare.mountedSlotIndices.isEmpty)
        XCTAssertEqual(bare.unrenderableSlotIndices, [0])
    }

    func test_removingASculpture_dropsOnlyItsEntity() async {
        let layer = makeLayer()
        await layer.apply([fixtureSculpture(slot: 0), fixtureSculpture(slot: 1)])
        let keptPosition = layer.position(atSlot: 0)

        await layer.apply([fixtureSculpture(slot: 0)])

        XCTAssertEqual(layer.mountedSlotIndices, [0])
        XCTAssertEqual(layer.root.children.count, 1)
        XCTAssertEqual(layer.position(atSlot: 0), keptPosition, "the surviving sculpture must not move")
    }

    func test_addingIntoAFreedSlot_mountsThere() async {
        let layer = makeLayer()
        await layer.apply([fixtureSculpture(slot: 0), fixtureSculpture(slot: 2)])
        await layer.apply([fixtureSculpture(slot: 0), fixtureSculpture(slot: 1), fixtureSculpture(slot: 2)])

        XCTAssertEqual(layer.mountedSlotIndices, [0, 1, 2])
        XCTAssertEqual(layer.position(atSlot: 1), PlaceholderRoomSlotTable.build().sculptureTransforms[1]?.position)
    }

    func test_sculpturesCarryNoCollisionComponent() async {
        let layer = makeLayer()
        await layer.apply([fixtureSculpture(slot: 0), fixtureSculpture(slot: 1), fixtureSculpture(slot: 2)])

        for child in layer.root.children {
            XCTAssertNil(child.components[CollisionComponent.self], "a sculpture must not become a movement obstacle")
            for grandchild in child.children {
                XCTAssertNil(grandchild.components[CollisionComponent.self])
            }
        }
    }

    func test_tearDown_removesEverything() async {
        let layer = makeLayer()
        await layer.apply([fixtureSculpture(slot: 0), fixtureSculpture(slot: 1)])

        layer.tearDown()

        XCTAssertEqual(layer.root.children.count, 0)
        XCTAssertTrue(layer.mountedSlotIndices.isEmpty)
        XCTAssertTrue(layer.unrenderableSlotIndices.isEmpty)
    }
}

@MainActor
final class RoomSculptureRuntimeTests: XCTestCase {

    private func waitUntil(_ timeout: TimeInterval = 20, _ condition: @escaping @MainActor () -> Bool) async {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition() && Date() < deadline {
            try? await Task.sleep(nanoseconds: 50_000_000)
        }
    }

    private func ownerController(photoCount: Int = 3) -> RealityKitSceneViewController {
        let content = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount)!
        let controller = RealityKitSceneViewController(content: content, photoPicker: FakePhotoPicker(photos: []))
        controller.loadViewIfNeeded()
        return controller
    }

    func test_owner_getsTheSculptureLayerAndCoordinator_andTheFixtureSculptureRenders() async {
        let controller = ownerController()
        controller.viewWillAppear(false)

        await waitUntil { controller.sculptureLayer?.mountedSlotIndices.isEmpty == false }

        XCTAssertNotNil(controller.sculptureCoordinator)
        XCTAssertEqual(controller.sculptureLayer?.mountedSlotIndices, [1])
        XCTAssertEqual(controller.sculptureCoordinator?.sculptures.map(\.slotIndex), [1])
        XCTAssertEqual(controller.sculptureCoordinator?.remainingCapacity, 2)
        controller.viewDidDisappear(false)
    }

    func test_visitor_seesSculptures_butGetsNoCoordinator() async {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 2)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .visitor, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: fixture.photoService, roomService: fixture.roomService,
            sculptureModels: fixture.sculptureModels, catalogService: fixture.catalogService
        )
        XCTAssertFalse(content.supportsSculptureEditing)

        let controller = RealityKitSceneViewController(content: content, photoPicker: FakePhotoPicker(photos: []))
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        await waitUntil { controller.sculptureLayer?.mountedSlotIndices.isEmpty == false }

        XCTAssertEqual(controller.sculptureLayer?.mountedSlotIndices, [1], "a visitor still sees the sculpture")
        XCTAssertNil(controller.sculptureCoordinator, "but cannot change it")
        controller.viewDidDisappear(false)
    }

    func test_theSculpturesAction_appearsOnlyWhileEditing() {
        let controller = ownerController()
        controller.viewWillAppear(false)

        func hasSculptureAction() -> Bool {
            (controller.navigationItem.rightBarButtonItems ?? [])
                .contains { $0.accessibilityIdentifier == "room-sculptures-action" }
        }

        XCTAssertFalse(hasSculptureAction(), "not before entering Edit Mode")
        controller.enterEditMode()
        XCTAssertTrue(hasSculptureAction())
        controller.exitEditMode()
        XCTAssertFalse(hasSculptureAction())
        controller.viewDidDisappear(false)
    }

    func test_withoutACatalog_noSculptureAffordanceIsCreated() {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 2)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .owner, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: fixture.photoService, roomService: fixture.roomService,
            sculptureModels: fixture.sculptureModels
        )
        XCTAssertFalse(content.supportsSculptureEditing)

        let controller = RealityKitSceneViewController(content: content, photoPicker: FakePhotoPicker(photos: []))
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        controller.enterEditMode()

        XCTAssertNil(controller.sculptureCoordinator)
        XCTAssertFalse(
            (controller.navigationItem.rightBarButtonItems ?? []).contains { $0.accessibilityIdentifier == "room-sculptures-action" }
        )
        controller.viewDidDisappear(false)
    }

    func test_addAndRemove_reachTheScene() async {
        let controller = ownerController()
        controller.viewWillAppear(false)
        await waitUntil { controller.sculptureLayer?.mountedSlotIndices == [1] }
        let coordinator = controller.sculptureCoordinator!

        _ = await coordinator.add(catalogID: RoomRenderingVerificationFixture.sculptureCatalogID)
        await waitUntil { controller.sculptureLayer?.mountedSlotIndices == [0, 1] }
        XCTAssertEqual(coordinator.sculptures.map(\.slotIndex), [0, 1], "the newcomer took the lowest free slot")

        _ = await coordinator.remove(slotIndex: 1)
        await waitUntil { controller.sculptureLayer?.mountedSlotIndices == [0] }
        XCTAssertEqual(coordinator.sculptures.map(\.slotIndex), [0], "and slot 1 is empty, slot 0 untouched")
        controller.viewDidDisappear(false)
    }

    func test_sculptureEdits_doNotDisturbPhotographs() async {
        let controller = ownerController(photoCount: 5)
        controller.viewWillAppear(false)
        await waitUntil { controller.texturedPhotoCount == 5 }
        let photoLayer = controller.photoLayer!
        let generationBefore = photoLayer.generation
        let orderBefore = controller.contentCoordinator!.room.photoSlots

        _ = await controller.sculptureCoordinator!.add(catalogID: RoomRenderingVerificationFixture.sculptureCatalogID)
        _ = await controller.sculptureCoordinator!.remove(slotIndex: 1)

        XCTAssertEqual(photoLayer.generation, generationBefore, "no photo remount")
        XCTAssertEqual(photoLayer.texturedAssetIDs.count, 5, "no texture lost or re-downloaded")
        XCTAssertEqual(controller.contentCoordinator?.room.photoSlots, orderBefore, "photo content untouched")
        XCTAssertEqual(controller.captionLayer?.caption(forAsset: "fixture-asset-0"), RoomRenderingVerificationFixture.seedCaption(forSlot: 0))
        controller.viewDidDisappear(false)
    }

    func test_leavingTheRoom_removesTheSculptureMachinery() async {
        let controller = ownerController()
        controller.viewWillAppear(false)
        await waitUntil { controller.sculptureLayer != nil }

        controller.viewDidDisappear(false)

        XCTAssertNil(controller.sculptureLayer)
        XCTAssertNil(controller.sculptureCoordinator)
    }
}
