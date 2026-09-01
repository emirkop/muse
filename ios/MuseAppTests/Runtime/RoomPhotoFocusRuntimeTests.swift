import RealityKit
import simd
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RoomPhotoFocusLayerTests: XCTestCase {
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

    private func placements(_ count: Int) -> [ResolvedPhotoPlacement] {
        let room = Room(
            id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") }
        )
        guard case .resolved(let resolved) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            XCTFail("must resolve")
            return []
        }
        return resolved
    }

    private func mountedLayer(_ count: Int) -> RoomPhotoLayer {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        _ = layer.mount(placements(count))
        return layer
    }

    func test_focusIsNilUntilSet() {
        let layer = mountedLayer(4)

        XCTAssertNil(layer.focusedPhotoAssetID)
    }

    func test_settingFocus_scalesThatPhotographOnly() {
        let layer = mountedLayer(4)

        layer.setFocused(assetID: "a1")

        XCTAssertEqual(layer.focusedPhotoAssetID, "a1")
    }

    func test_focusIsTheSubtlestAffordance() {
        XCTAssertLessThan(
            RoomPhotoLayer.focusScale, RoomPhotoLayer.targetScale,
            "`02` calls it a *subtle* affordance, and it happens continuously while walking"
        )
        XCTAssertLessThan(RoomPhotoLayer.targetScale, RoomPhotoLayer.liftScale)
        XCTAssertGreaterThan(RoomPhotoLayer.focusScale, 1)
    }

    func test_focusMovesExclusively() {
        let layer = mountedLayer(4)

        layer.setFocused(assetID: "a0")
        layer.setFocused(assetID: "a2")

        XCTAssertEqual(layer.focusedPhotoAssetID, "a2")
    }

    func test_clearingFocus() {
        let layer = mountedLayer(3)
        layer.setFocused(assetID: "a1")

        layer.setFocused(assetID: nil)

        XCTAssertNil(layer.focusedPhotoAssetID)
    }

    func test_liftOutranksFocus() {
        let layer = mountedLayer(4)

        layer.setFocused(assetID: "a1")
        layer.setLifted(assetID: "a1")

        XCTAssertEqual(layer.liftedAssetIDForTesting, "a1")
        XCTAssertEqual(layer.focusedPhotoAssetID, "a1")
        XCTAssertEqual(layer.mountedAssetIDs.count, 4, "no photograph was rebuilt by the overlap")
    }

    func test_endingADrag_leavesFocusIntact() {
        let layer = mountedLayer(4)
        layer.setFocused(assetID: "a1")
        layer.setLifted(assetID: "a1")
        layer.setTarget(assetID: "a2")

        layer.clearInteractionFeedback()

        XCTAssertNil(layer.liftedAssetIDForTesting)
        XCTAssertNil(layer.targetAssetIDForTesting)
        XCTAssertEqual(
            layer.focusedPhotoAssetID, "a1",
            "focus is where the viewer stands, which finishing a drag does not change"
        )
    }

    func test_focusDoesNotSurviveTeardown() {
        let layer = mountedLayer(3)
        layer.setFocused(assetID: "a0")

        layer.tearDown()

        XCTAssertNil(layer.focusedPhotoAssetID)
    }

    func test_focusingCausesNoRemountAndNoTextureChange() async {
        let layer = mountedLayer(6)
        let generation = layer.generation
        let image = try! PhotoTextureDecoder.decode(
            PhotoTextureDecoderTests.jpeg(width: 300, height: 200), maxLongEdge: 256
        )
        for slot in 0..<6 {
            await layer.apply(image, forAsset: "a\(slot)", generation: generation)
        }
        let entities = layer.root.children.map { ObjectIdentifier($0) }

        for slot in 0..<6 {
            layer.setFocused(assetID: "a\(slot)")
        }
        layer.setFocused(assetID: nil)

        XCTAssertEqual(layer.generation, generation, "focus is not a remount")
        XCTAssertEqual(layer.root.children.map { ObjectIdentifier($0) }, entities, "no entity was rebuilt")
        XCTAssertEqual(layer.texturedAssetIDs.count, 6, "every texture survived")
    }

    func test_focusingAnUnmountedPhotograph_isRecordedButDrawsNothing() {
        let layer = mountedLayer(2)

        layer.setFocused(assetID: "not-in-this-room")

        XCTAssertEqual(layer.focusedPhotoAssetID, "not-in-this-room")
        XCTAssertEqual(layer.mountedAssetIDs.count, 2)
    }

    func test_mountedPlacementsReflectTheLiveLayout() {
        let layer = mountedLayer(5)

        XCTAssertEqual(Set(layer.mountedPlacements().map(\.photoAssetID)), layer.mountedAssetIDs)

        let room = Room(
            id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: (0..<5).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") }
        )
        let swapped = room.replacingPhotoSlots(RoomPhotoOrder.swapping(room.photoSlots, from: 0, to: 4))
        guard case .resolved(let after) = RoomPlacementResolver.resolve(room: swapped, slotTable: table) else {
            return XCTFail("must resolve")
        }
        layer.relayout(after)

        let byAsset = Dictionary(uniqueKeysWithValues: layer.mountedPlacements().map { ($0.photoAssetID, $0) })
        XCTAssertEqual(byAsset["a0"]?.slotIndex, 4, "focus sees the photograph where the reorder put it")
        XCTAssertEqual(byAsset["a4"]?.slotIndex, 0)
    }
}

@MainActor
final class CameraFocusZoomTests: XCTestCase {
    private func controller(mode: MuseumCameraMode = .firstPerson) -> RealityKitCameraController {
        RealityKitCameraController(mode: mode, subject: MuseumCameraSubject())
    }

    func test_restingFieldOfViewIsTheStatedBaseline() {
        let camera = controller()

        XCTAssertEqual(camera.focusZoom, 0)
        XCTAssertEqual(camera.currentFieldOfViewDegrees, MuseumCameraConfiguration.default.fieldOfViewDegrees)
    }

    func test_zoomIsNarrowerThanRest() {
        let configuration = MuseumCameraConfiguration.default

        XCTAssertLessThan(
            configuration.focusedFieldOfViewDegrees, configuration.fieldOfViewDegrees,
            "zooming a perspective camera means narrowing its field of view"
        )
    }

    func test_zoomEasesInRatherThanSnapping() {
        let camera = controller()

        camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60)
        let afterOneFrame = camera.focusZoom

        XCTAssertGreaterThan(afterOneFrame, 0)
        XCTAssertLessThan(afterOneFrame, 0.5, "one frame must not complete the transition")
    }

    func test_zoomConvergesExactlyAndStops() {
        let camera = controller()

        for _ in 0..<240 { camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.focusZoom, 1, "the eased value must land exactly, not a hair short")
        XCTAssertEqual(
            camera.currentFieldOfViewDegrees,
            MuseumCameraConfiguration.default.focusedFieldOfViewDegrees,
            accuracy: 0.0001
        )
    }

    func test_zoomEasesBackOutToExactlyRest() {
        let camera = controller()
        for _ in 0..<240 { camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }

        for _ in 0..<240 { camera.advanceFocusZoom(towards: false, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.focusZoom, 0)
        XCTAssertEqual(
            camera.currentFieldOfViewDegrees, MuseumCameraConfiguration.default.fieldOfViewDegrees, accuracy: 0.0001
        )
    }

    func test_zoomIsFrameRateIndependent() {
        let fast = controller()
        let slow = controller()

        for _ in 0..<60 { fast.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }
        for _ in 0..<30 { slow.advanceFocusZoom(towards: true, deltaTime: 1.0 / 30) }

        XCTAssertEqual(fast.focusZoom, slow.focusZoom, accuracy: 0.02, "one second of easing, either way")
    }

    func test_zeroDeltaTimeHolds() {
        let camera = controller()
        camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60)
        let before = camera.focusZoom

        camera.advanceFocusZoom(towards: true, deltaTime: 0)

        XCTAssertEqual(camera.focusZoom, before, "no time passed, so nothing moved")
    }

    func test_thirdPersonNeverZooms() {
        let camera = controller(mode: .thirdPerson)

        for _ in 0..<120 { camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.focusZoom, 0)
    }

    func test_switchingToThirdPersonMidZoom_easesBackToRest() {
        let camera = controller()
        for _ in 0..<240 { camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }
        XCTAssertEqual(camera.focusZoom, 1)

        camera.setMode(.thirdPerson)
        for _ in 0..<240 { camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.focusZoom, 0, "a mode switch must not leave third person narrowed")
    }

    func test_disablingZoom_keepsItAtRest() {
        let camera = controller()
        camera.isFocusZoomEnabled = false

        for _ in 0..<120 { camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.focusZoom, 0, "Reduce Motion keeps the highlight but drops the movement")
        XCTAssertEqual(camera.currentFieldOfViewDegrees, MuseumCameraConfiguration.default.fieldOfViewDegrees)
    }

    func test_disablingZoomMidFlight_returnsImmediatelyToRest() {
        let camera = controller()
        for _ in 0..<240 { camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }

        camera.isFocusZoomEnabled = false

        XCTAssertEqual(camera.focusZoom, 0, "turning the zoom off must not strand the camera narrowed")
        XCTAssertEqual(camera.currentFieldOfViewDegrees, MuseumCameraConfiguration.default.fieldOfViewDegrees)
    }

    func test_modeSwitchStillPreservesTheSubject() {
        let camera = controller()
        camera.subject = MuseumCameraSubject(position: SIMD3<Float>(1, 0, 2), yaw: 0.8)
        let before = camera.subject

        camera.setMode(.thirdPerson)
        camera.setMode(.firstPerson)

        XCTAssertEqual(camera.subject, before)
    }
}

@MainActor
final class RoomPhotoFocusIntegrationTests: XCTestCase {
    private func controller(photoCount: Int = 28) throws -> RealityKitSceneViewController {
        let content = try XCTUnwrap(RoomRenderingVerificationFixture.makeContent(photoCount: photoCount))
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        return controller
    }

    private func viewer(facing placement: ResolvedPhotoPlacement, distance: Float = 1.6) -> MuseumCameraSubject {
        let normal = placement.transform.rotation.act(SIMD3<Float>(0, 0, 1))
        let standing = placement.transform.position + normal * distance
        let facing = -normal
        let yaw = atan2(-facing.x, -facing.z)
        return MuseumCameraSubject(position: SIMD3<Float>(standing.x, 0, standing.z), yaw: yaw)
    }

    func test_atSpawn_nothingIsFocusedAndTheCameraIsAtRest() throws {
        let controller = try self.controller()

        controller.testAdvanceFrames(5)

        XCTAssertNil(controller.photoLayer?.focusedPhotoAssetID, "arriving must not focus anything")
        XCTAssertEqual(controller.cameraController?.focusZoom, 0)
    }

    func test_walkingUpToAPhotograph_focusesItAndZoomsIn() throws {
        let controller = try self.controller()
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })

        controller.testMoveViewer(to: viewer(facing: target))
        controller.testAdvanceFrames(240)

        XCTAssertEqual(controller.photoLayer?.focusedPhotoAssetID, target.photoAssetID)
        XCTAssertEqual(controller.cameraController?.focusZoom, 1, "the zoom eases all the way in")
    }

    func test_walkingAway_releasesFocusAndTheZoom() throws {
        let controller = try self.controller()
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })
        controller.testMoveViewer(to: viewer(facing: target))
        controller.testAdvanceFrames(240)
        XCTAssertNotNil(controller.photoLayer?.focusedPhotoAssetID)

        controller.testMoveViewer(to: PlaceholderRoom.spawnPoint)
        controller.testAdvanceFrames(240)

        XCTAssertNil(controller.photoLayer?.focusedPhotoAssetID)
        XCTAssertEqual(controller.cameraController?.focusZoom, 0)
    }

    func test_standingCloseButFacingAway_focusesNothing() throws {
        let controller = try self.controller()
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })
        var away = viewer(facing: target)
        away.yaw += .pi

        controller.testMoveViewer(to: away)
        controller.testAdvanceFrames(5)

        XCTAssertNil(controller.photoLayer?.focusedPhotoAssetID, "proximity alone is not the rule")
    }

    func test_focusWorksForAVisitor() throws {
        let owned = try XCTUnwrap(RoomRenderingVerificationFixture.makeContent(photoCount: 6))
        let asVisitor = RoomRuntimeContent(
            roomID: owned.roomID, accessToken: owned.accessToken, geometry: owned.geometry,
            viewerRole: .visitor, room: owned.room, slotTable: owned.slotTable,
            placements: owned.placements, textures: owned.textures
        )
        let controller = RealityKitSceneViewController(content: asVisitor)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        let target = try XCTUnwrap(asVisitor.placements.first { $0.anchor.wall == .focal })

        controller.testMoveViewer(to: viewer(facing: target))
        controller.testAdvanceFrames(10)

        XCTAssertNil(controller.editMode, "still no Edit Mode for a visitor")
        XCTAssertEqual(controller.photoLayer?.focusedPhotoAssetID, target.photoAssetID)
    }

    func test_focusIsSuppressedWhileAPhotographIsLifted() throws {
        let controller = try self.controller()
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })
        controller.testMoveViewer(to: viewer(facing: target))
        XCTAssertNotNil(controller.photoLayer?.focusedPhotoAssetID)

        controller.photoLayer?.setLifted(assetID: target.photoAssetID)
        controller.testAdvanceFrames(2)

        XCTAssertNil(controller.photoLayer?.focusedPhotoAssetID, "a drag is manipulating, not looking")

        controller.photoLayer?.setLifted(assetID: nil)
        controller.testAdvanceFrames(2)

        XCTAssertEqual(
            controller.photoLayer?.focusedPhotoAssetID, target.photoAssetID,
            "and focus comes back once the drag ends"
        )
    }

    func test_focusNeverMovesTheViewer() throws {
        let controller = try self.controller()
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })
        let standing = viewer(facing: target)

        controller.testMoveViewer(to: standing)
        controller.testAdvanceFrames(120)

        XCTAssertEqual(controller.movementController.subject, standing, "focus is an affordance, not a camera move")
    }

    func test_focusCausesNoTextureWorkInTheLiveLoop() throws {
        let controller = try self.controller()
        let generation = try XCTUnwrap(controller.photoLayer?.generation)
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })

        controller.testMoveViewer(to: viewer(facing: target))
        controller.testAdvanceFrames(120)

        XCTAssertEqual(controller.photoLayer?.generation, generation, "walking up to a photograph is not a remount")
    }

    func test_theSkeletonHasNoFocusAndNoZoom() {
        let controller = RealityKitSceneViewController(content: nil)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        controller.testMoveViewer(to: MuseumCameraSubject(position: SIMD3<Float>(0, 0, -3), yaw: 0))
        controller.testAdvanceFrames(60)

        XCTAssertNil(controller.photoLayer)
        XCTAssertEqual(controller.cameraController?.focusZoom, 0)
    }

    func test_focusIsTornDownWithTheScene() throws {
        let controller = try self.controller()
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })
        controller.testMoveViewer(to: viewer(facing: target))
        XCTAssertNotNil(controller.photoLayer?.focusedPhotoAssetID)

        controller.viewDidDisappear(false)

        XCTAssertNil(controller.photoLayer, "no focus state can outlive the scene")
    }
}
