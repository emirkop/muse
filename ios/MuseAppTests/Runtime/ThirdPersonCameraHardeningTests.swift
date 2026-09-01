import RealityKit
import simd
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class ThirdPersonCameraHardeningTests: XCTestCase {
    private var arView: ARView!
    private var anchor: AnchorEntity!

    override func setUp() {
        arView = ARView(frame: CGRect(x: 0, y: 0, width: 200, height: 200), cameraMode: .nonAR, automaticallyConfigureSession: false)
        anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        arView = nil
        anchor = nil
    }

    private func attachedCamera(
        configuration: MuseumCameraConfiguration = .default,
        subject: MuseumCameraSubject = MuseumCameraSubject()
    ) -> RealityKitCameraController {
        let camera = RealityKitCameraController(mode: .thirdPerson, subject: subject, configuration: configuration)
        camera.attach(to: anchor, in: arView.scene)
        return camera
    }

    private func addWall(at position: SIMD3<Float>, size: SIMD3<Float>) {
        let wall = ModelEntity(mesh: .generateBox(size: size))
        wall.position = position
        wall.generateCollisionShapes(recursive: false)
        anchor.addChild(wall)
    }

    private func fullBoomLength(_ configuration: MuseumCameraConfiguration = .default) -> Float {
        length(SIMD3<Float>(0, configuration.thirdPersonHeight, configuration.thirdPersonDistance))
    }

    private func reapplySubject(_ camera: RealityKitCameraController) {
        camera.subject = MuseumCameraSubject(position: camera.subject.position, yaw: camera.subject.yaw)
    }

    // MARK: - Unobstructed behaviour is unchanged

    func test_withNoGeometry_theBoomIsFullLength() {
        let camera = attachedCamera()

        XCTAssertEqual(camera.resolvedBoomLength(), fullBoomLength(), accuracy: 0.0001)
    }

    func test_withNoScene_theBoomIsFullLength() {
        let camera = RealityKitCameraController(mode: .thirdPerson)

        XCTAssertEqual(camera.resolvedBoomLength(), fullBoomLength(), accuracy: 0.0001)
    }

    func test_theUnobstructedIdealIsUnchangedFrom() {
        let configuration = MuseumCameraConfiguration(eyeHeight: 1.6, thirdPersonDistance: 3, thirdPersonHeight: 0.8)
        let subject = MuseumCameraSubject(position: SIMD3<Float>(2, 0, -1), yaw: 0.7)
        let camera = RealityKitCameraController(mode: .thirdPerson, subject: subject, configuration: configuration)

        let expected = subject.eyePosition(eyeHeight: 1.6)
            + subject.orientation.act(SIMD3<Float>(0, 0.8, 3.0))

        let actual = camera.desiredThirdPersonPosition()
        XCTAssertEqual(actual.x, expected.x, accuracy: 0.0005)
        XCTAssertEqual(actual.y, expected.y, accuracy: 0.0005)
        XCTAssertEqual(actual.z, expected.z, accuracy: 0.0005)
    }

    // MARK: - The camera is a volume, not a point

    func test_anObstacleOffTheCentreLine_missesARayButShortensTheBoom() {
        let configuration = MuseumCameraConfiguration(
            eyeHeight: 1.6, thirdPersonDistance: 3, thirdPersonHeight: 0, cameraProbeRadius: 0.18
        )
        let camera = attachedCamera(configuration: configuration)
        let eye = camera.subject.eyePosition(eyeHeight: configuration.eyeHeight)
        let direction = camera.boomDirection()

        let sideOffset = configuration.cameraProbeRadius * 0.75
        let postThickness: Float = 0.1
        addWall(
            at: eye + direction * 1.5 + SIMD3<Float>(sideOffset + postThickness / 2, 0, 0),
            size: SIMD3<Float>(postThickness, 2, postThickness)
        )

        let rayHits = arView.scene.raycast(
            origin: eye, direction: direction, length: fullBoomLength(configuration), query: .nearest
        )
        let resolved = camera.resolvedBoomLength()

        XCTAssertTrue(rayHits.isEmpty, "the post is deliberately off the centre line — a ray misses it")
        XCTAssertLessThan(
            resolved, fullBoomLength(configuration),
            "the swept probe must catch what the ray missed, or the near plane clips the post"
        )
    }

    func test_geometryDirectlyBehind_shortensTheBoom() {
        let camera = attachedCamera()
        let eye = camera.subject.eyePosition(eyeHeight: MuseumCameraConfiguration.default.eyeHeight)
        addWall(at: eye + camera.boomDirection() * 1.5, size: SIMD3<Float>(4, 4, 0.1))

        XCTAssertLessThan(camera.resolvedBoomLength(), fullBoomLength())
    }

    // MARK: - Minimum standoff

    func test_geometryPressedAgainstTheSubject_clampsToTheMinimumStandoff() {
        let configuration = MuseumCameraConfiguration.default
        let camera = attachedCamera(configuration: configuration)
        let eye = camera.subject.eyePosition(eyeHeight: configuration.eyeHeight)
        addWall(at: eye + camera.boomDirection() * 0.25, size: SIMD3<Float>(4, 4, 0.1))

        let resolved = camera.resolvedBoomLength()

        XCTAssertEqual(
            resolved, configuration.thirdPersonMinimumDistance, accuracy: 0.0001,
            "the boom holds at the standoff rather than collapsing onto the subject"
        )
        XCTAssertGreaterThan(resolved, 0)
    }

    func test_theBoomIsNeverShorterThanTheStandoff_fromAnyStandingPointInTheFixtureRoom() {
        let configuration = MuseumCameraConfiguration.default
        anchor.addChild(PlaceholderRoom.build())
        let camera = attachedCamera(configuration: configuration)

        for xStep in -3...3 {
            for zStep in -4...4 {
                for yawStep in 0..<8 {
                    camera.subject = MuseumCameraSubject(
                        position: SIMD3<Float>(Float(xStep), 0, Float(zStep)),
                        yaw: Float(yawStep) * .pi / 4
                    )
                    XCTAssertGreaterThanOrEqual(
                        camera.thirdPersonDistance,
                        configuration.thirdPersonMinimumDistance - 0.0001,
                        "standing at (\(xStep), \(zStep)) yaw \(yawStep)"
                    )
                }
            }
        }
    }

    func test_theCameraStaysInsideTheFixtureRoom_fromEveryStandingPoint() {
        let configuration = MuseumCameraConfiguration.default
        anchor.addChild(PlaceholderRoom.build())
        let camera = attachedCamera(configuration: configuration)
        let halfWidth = PlaceholderRoom.width / 2
        let halfDepth = PlaceholderRoom.depth / 2

        for xStep in -3...3 {
            for zStep in -4...4 {
                for yawStep in 0..<8 {
                    let subject = MuseumCameraSubject(
                        position: SIMD3<Float>(Float(xStep), 0, Float(zStep)),
                        yaw: Float(yawStep) * .pi / 4
                    )
                    camera.subject = subject
                    let cameraPosition = subject.eyePosition(eyeHeight: configuration.eyeHeight)
                        + camera.boomDirection() * camera.thirdPersonDistance

                    XCTAssertLessThan(
                        abs(cameraPosition.x), halfWidth,
                        "camera left the room in X at (\(xStep), \(zStep)) yaw \(yawStep)"
                    )
                    XCTAssertLessThan(
                        abs(cameraPosition.z), halfDepth,
                        "camera left the room in Z at (\(xStep), \(zStep)) yaw \(yawStep)"
                    )
                    XCTAssertGreaterThan(cameraPosition.y, 0)
                    XCTAssertLessThan(cameraPosition.y, PlaceholderRoom.height)
                }
            }
        }
    }

    // MARK: - Instant pull-in, eased recovery

    func test_pullInIsInstant_withoutWaitingForATick() {
        let camera = attachedCamera()
        XCTAssertEqual(camera.thirdPersonDistance, fullBoomLength(), accuracy: 0.0001)

        let eye = camera.subject.eyePosition(eyeHeight: MuseumCameraConfiguration.default.eyeHeight)
        addWall(at: eye + camera.boomDirection() * 1.2, size: SIMD3<Float>(4, 4, 0.1))
        reapplySubject(camera)

        XCTAssertLessThan(
            camera.thirdPersonDistance, fullBoomLength(),
            "the boom must already be short on the very frame the obstacle appears"
        )
    }

    func test_recoveryIsEasedNotInstant() {
        let camera = attachedCamera()
        let eye = camera.subject.eyePosition(eyeHeight: MuseumCameraConfiguration.default.eyeHeight)
        let wall = ModelEntity(mesh: .generateBox(size: SIMD3<Float>(4, 4, 0.1)))
        wall.position = eye + camera.boomDirection() * 1.2
        wall.generateCollisionShapes(recursive: false)
        anchor.addChild(wall)
        reapplySubject(camera)
        let pulledIn = camera.thirdPersonDistance
        XCTAssertLessThan(pulledIn, fullBoomLength())

        wall.removeFromParent()
        camera.advanceThirdPersonFraming(towards: false, deltaTime: 1.0 / 60)
        let afterOneFrame = camera.thirdPersonDistance

        XCTAssertGreaterThan(afterOneFrame, pulledIn, "the boom starts growing back")
        XCTAssertLessThan(afterOneFrame, fullBoomLength(), "but must not snap out in a single frame")
    }

    func test_recoveryConvergesExactlyToFullLength() {
        let camera = attachedCamera()
        let eye = camera.subject.eyePosition(eyeHeight: MuseumCameraConfiguration.default.eyeHeight)
        let wall = ModelEntity(mesh: .generateBox(size: SIMD3<Float>(4, 4, 0.1)))
        wall.position = eye + camera.boomDirection() * 1.2
        wall.generateCollisionShapes(recursive: false)
        anchor.addChild(wall)
        reapplySubject(camera)
        wall.removeFromParent()

        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: false, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.thirdPersonDistance, fullBoomLength(), accuracy: 0.0005)
    }

    func test_recoveryIsFrameRateIndependent() {
        func pulledInCamera() -> (RealityKitCameraController, ModelEntity) {
            let camera = attachedCamera()
            let eye = camera.subject.eyePosition(eyeHeight: MuseumCameraConfiguration.default.eyeHeight)
            let wall = ModelEntity(mesh: .generateBox(size: SIMD3<Float>(4, 4, 0.1)))
            wall.position = eye + camera.boomDirection() * 1.2
            wall.generateCollisionShapes(recursive: false)
            anchor.addChild(wall)
            reapplySubject(camera)
            wall.removeFromParent()
            return (camera, wall)
        }

        let (fast, _) = pulledInCamera()
        for _ in 0..<60 { fast.advanceThirdPersonFraming(towards: false, deltaTime: 1.0 / 60) }
        let (slow, _) = pulledInCamera()
        for _ in 0..<30 { slow.advanceThirdPersonFraming(towards: false, deltaTime: 1.0 / 30) }

        XCTAssertEqual(fast.thirdPersonDistance, slow.thirdPersonDistance, accuracy: 0.05)
    }

    func test_turningIsNotEased() {
        let camera = attachedCamera()
        let before = camera.boomDirection()

        camera.subject = MuseumCameraSubject(position: .zero, yaw: .pi / 2)

        let after = camera.boomDirection()
        XCTAssertNotEqual(before, after)
        let expected = camera.subject.orientation.act(SIMD3<Float>(0, 0.8, 3.0))
        XCTAssertEqual(after.x, normalize(expected).x, accuracy: 0.0005)
        XCTAssertEqual(after.z, normalize(expected).z, accuracy: 0.0005)
    }

    // MARK: - Auto-framing (`02` step 3)

    func test_framingShortensTheBoom() {
        let camera = attachedCamera()

        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.framingAmount, 1)
        XCTAssertLessThan(
            camera.thirdPersonDistance, fullBoomLength(),
            "`02`: approaching a photo may auto-adjust the camera framing"
        )
    }

    func test_framingEasesInRatherThanSnapping() {
        let camera = attachedCamera()

        camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60)

        XCTAssertGreaterThan(camera.framingAmount, 0)
        XCTAssertLessThan(camera.framingAmount, 0.5)
    }

    func test_framingEasesBackOutExactly() {
        let camera = attachedCamera()
        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60) }

        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: false, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.framingAmount, 0)
        XCTAssertEqual(camera.thirdPersonDistance, fullBoomLength(), accuracy: 0.0005)
    }

    func test_framingNeverBreachesTheMinimumStandoff() {
        let configuration = MuseumCameraConfiguration(
            thirdPersonDistance: 1.0, thirdPersonHeight: 0,
            thirdPersonMinimumDistance: 0.85, thirdPersonFramingFraction: 0.95
        )
        let camera = attachedCamera(configuration: configuration)

        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertGreaterThanOrEqual(camera.thirdPersonDistance, configuration.thirdPersonMinimumDistance - 0.0001)
    }

    func test_zeroDeltaTimeHolds() {
        let camera = attachedCamera()
        camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60)
        let before = camera.framingAmount

        camera.advanceThirdPersonFraming(towards: true, deltaTime: 0)

        XCTAssertEqual(camera.framingAmount, before)
    }

    // MARK: - First person is untouched ( preserved)

    func test_framingNeverAppliesInFirstPerson() {
        let camera = RealityKitCameraController(mode: .firstPerson)
        camera.attach(to: anchor, in: arView.scene)

        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.framingAmount, 0, "third-person framing is not a first-person behaviour")
    }

    func test_framingDoesNotTouchTheFieldOfView() {
        let camera = attachedCamera()
        let restingFOV = camera.currentFieldOfViewDegrees

        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(
            camera.currentFieldOfViewDegrees, restingFOV,
            "zoom is first-person only; framing must not double up on it"
        )
    }

    func test_ZoomStillDoesNotApplyInThirdPerson() {
        let camera = attachedCamera()

        for _ in 0..<600 { camera.advanceFocusZoom(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.focusZoom, 0, "rule is preserved exactly")
    }

    // MARK: - Mode switching

    func test_enteringThirdPerson_doesNotInheritAStaleShortBoom() {
        let camera = attachedCamera()
        let eye = camera.subject.eyePosition(eyeHeight: MuseumCameraConfiguration.default.eyeHeight)
        let wall = ModelEntity(mesh: .generateBox(size: SIMD3<Float>(4, 4, 0.1)))
        wall.position = eye + camera.boomDirection() * 1.2
        wall.generateCollisionShapes(recursive: false)
        anchor.addChild(wall)
        reapplySubject(camera)
        XCTAssertLessThan(camera.thirdPersonDistance, fullBoomLength())

        camera.setMode(.firstPerson)
        wall.removeFromParent()
        camera.setMode(.thirdPerson)

        XCTAssertEqual(
            camera.thirdPersonDistance, fullBoomLength(), accuracy: 0.0005,
            "re-entering third person starts from what geometry allows now"
        )
    }

    func test_modeSwitchStillPreservesTheSubjectExactly() {
        let camera = attachedCamera(subject: MuseumCameraSubject(position: SIMD3<Float>(1, 0, 2), yaw: 0.9))
        let before = camera.subject

        camera.setMode(.firstPerson)
        camera.setMode(.thirdPerson)

        XCTAssertEqual(camera.subject, before, "invariant survives ")
    }

    func test_framingDecaysWhenLeavingThirdPerson() {
        let camera = attachedCamera()
        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60) }
        XCTAssertEqual(camera.framingAmount, 1)

        camera.setMode(.firstPerson)
        for _ in 0..<600 { camera.advanceThirdPersonFraming(towards: true, deltaTime: 1.0 / 60) }

        XCTAssertEqual(camera.framingAmount, 0)
    }
}

@MainActor
final class ThirdPersonNavigationIntegrationTests: XCTestCase {

    private func roomController() throws -> RealityKitSceneViewController {
        let content = try XCTUnwrap(RoomRenderingVerificationFixture.makeContent(photoCount: 28))
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        controller.cameraController?.setMode(.thirdPerson)
        return controller
    }

    private func viewer(facing placement: ResolvedPhotoPlacement, distance: Float = 1.6) -> MuseumCameraSubject {
        let normal = placement.transform.rotation.act(SIMD3<Float>(0, 0, 1))
        let standing = placement.transform.position + normal * distance
        let facing = -normal
        return MuseumCameraSubject(
            position: SIMD3<Float>(standing.x, 0, standing.z),
            yaw: atan2(-facing.x, -facing.z)
        )
    }

    func test_approachingAPhotographInThirdPerson_framesItWithoutZooming() throws {
        let controller = try roomController()
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })
        let restingFOV = try XCTUnwrap(controller.cameraController?.currentFieldOfViewDegrees)

        controller.testMoveViewer(to: viewer(facing: target))
        controller.testAdvanceFrames(600)

        XCTAssertEqual(controller.photoLayer?.focusedPhotoAssetID, target.photoAssetID)
        XCTAssertEqual(controller.cameraController?.framingAmount, 1, "third person frames")
        XCTAssertEqual(
            controller.cameraController?.currentFieldOfViewDegrees, restingFOV,
            "and does not zoom — zoom is first-person only"
        )
        XCTAssertEqual(controller.cameraController?.focusZoom, 0)
    }

    func test_walkingAwayInThirdPerson_releasesTheFraming() throws {
        let controller = try roomController()
        let target = try XCTUnwrap(controller.content?.placements.first { $0.anchor.wall == .focal })
        controller.testMoveViewer(to: viewer(facing: target))
        controller.testAdvanceFrames(600)
        XCTAssertEqual(controller.cameraController?.framingAmount, 1)

        controller.testMoveViewer(to: PlaceholderRoom.spawnPoint)
        controller.testAdvanceFrames(600)

        XCTAssertEqual(controller.cameraController?.framingAmount, 0)
    }

    func test_inFirstPerson_theSameApproachStillZoomsAndDoesNotFrame() throws {
        let content = try XCTUnwrap(RoomRenderingVerificationFixture.makeContent(photoCount: 28))
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        let target = try XCTUnwrap(content.placements.first { $0.anchor.wall == .focal })

        controller.testMoveViewer(to: viewer(facing: target))
        controller.testAdvanceFrames(600)

        XCTAssertEqual(controller.cameraController?.focusZoom, 1, " preserved")
        XCTAssertEqual(controller.cameraController?.framingAmount, 0, "and framing stays out of first person")
    }

    func test_theSkeletonStillTicksTheBoomRecovery() {
        let controller = RealityKitSceneViewController(content: nil)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        controller.cameraController?.setMode(.thirdPerson)

        controller.testMoveViewer(
            to: MuseumCameraSubject(position: SIMD3<Float>(0, 0, PlaceholderRoom.depth / 2 - 0.6), yaw: 0)
        )
        let pulledIn = try? XCTUnwrap(controller.cameraController?.thirdPersonDistance)

        controller.testMoveViewer(to: MuseumCameraSubject(position: .zero, yaw: 0))
        controller.testAdvanceFrames(600)
        let recovered = try? XCTUnwrap(controller.cameraController?.thirdPersonDistance)

        XCTAssertNotNil(pulledIn)
        XCTAssertNotNil(recovered)
        XCTAssertGreaterThan(recovered ?? 0, pulledIn ?? 0, "the boom recovers once nothing obstructs it")
    }

    func test_theLobbyGetsTheSameHardening() throws {
        let content = try XCTUnwrap(
            LobbyRenderingVerificationFixture.makeContent(roomCount: 5, viewerRole: .owner)
        )
        let controller = RealityKitLobbyViewController(content: content) { _ in }
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        controller.cameraController?.setMode(.thirdPerson)
        let configuration = MuseumCameraConfiguration.default

        let distance = try XCTUnwrap(controller.cameraController?.thirdPersonDistance)
        let full = length(SIMD3<Float>(0, configuration.thirdPersonHeight, configuration.thirdPersonDistance))

        XCTAssertLessThan(distance, full, "the back wall must shorten the boom at the Lobby's spawn point")
        XCTAssertGreaterThanOrEqual(
            distance, configuration.thirdPersonMinimumDistance - 0.0001,
            "and never below the standoff"
        )
    }

    func test_theLobbyCardFocusDoesNotFrameTheCamera() throws {
        let content = try XCTUnwrap(
            LobbyRenderingVerificationFixture.makeContent(roomCount: 5, viewerRole: .owner)
        )
        let controller = RealityKitLobbyViewController(content: content) { _ in }
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        controller.cameraController?.setMode(.thirdPerson)
        let card = try XCTUnwrap(content.placements.first)

        controller.cardInteraction?.updateFocus(viewerPosition: card.transform.position)

        XCTAssertEqual(controller.cardLayer?.focusedRoomID, card.roomID)
        XCTAssertEqual(
            controller.cameraController?.framingAmount, 0,
            "`02` scopes auto-framing to walls and photographs; card focus is 's entry affordance"
        )
    }
}
