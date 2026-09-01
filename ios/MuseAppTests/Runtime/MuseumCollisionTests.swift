import RealityKit
import simd
import XCTest
@testable import MuseApp

@MainActor
final class MuseumCollisionTests: XCTestCase {
    private let accuracy: Float = 0.001

    private func makeRoomScene() -> (ARView, RealityKitCollisionResolver) {
        let arView = ARView(frame: CGRect(x: 0, y: 0, width: 100, height: 100), cameraMode: .nonAR, automaticallyConfigureSession: false)
        let anchor = AnchorEntity(world: .zero)
        anchor.addChild(PlaceholderRoom.build())
        arView.scene.addAnchor(anchor)
        return (arView, RealityKitCollisionResolver(scene: arView.scene))
    }

    // MARK: - Wall clipping

    func test_movingIntoAWall_isBlockedBeforeReachingIt() {
        let (arView, resolver) = makeRoomScene()
        defer { _ = arView }

        let start = SIMD3<Float>(0, 0, 0)
        let throughWall = SIMD3<Float>(-20, 0, 0)

        let resolved = resolver.resolve(from: start, to: throughWall)

        XCTAssertGreaterThan(
            resolved.x,
            -PlaceholderRoom.width / 2,
            "the body must not pass through the wall"
        )
        XCTAssertLessThan(resolved.x, start.x, "movement toward the wall should still make some progress")
    }

    func test_movingInOpenSpace_isUnobstructed() {
        let (arView, resolver) = makeRoomScene()
        defer { _ = arView }

        let start = SIMD3<Float>(0, 0, 2)
        let desired = SIMD3<Float>(0, 0, 1)

        let resolved = resolver.resolve(from: start, to: desired)

        XCTAssertEqual(resolved.x, desired.x, accuracy: accuracy)
        XCTAssertEqual(resolved.z, desired.z, accuracy: accuracy, "a move well inside the room must not be altered")
    }

    func test_movingDiagonallyIntoAWall_slidesAlongIt() {
        let (arView, resolver) = makeRoomScene()
        defer { _ = arView }

        let start = SIMD3<Float>(-3.4, 0, 0)
        let desired = start + SIMD3<Float>(-1.0, 0, -1.0)

        let resolved = resolver.resolve(from: start, to: desired)

        XCTAssertLessThan(resolved.z, start.z - 0.1, "the along-wall component of movement must survive")
        XCTAssertGreaterThan(resolved.x, -PlaceholderRoom.width / 2, "the into-wall component must still be blocked")
    }

    func test_repeatedlyPressingIntoAWall_neverAccumulatesThrough() {
        let (arView, resolver) = makeRoomScene()
        defer { _ = arView }

        var position = SIMD3<Float>(0, 0, 0)
        for _ in 0..<200 {
            position = resolver.resolve(from: position, to: position + SIMD3<Float>(-0.1, 0, 0))
        }

        XCTAssertGreaterThan(
            position.x,
            -PlaceholderRoom.width / 2,
            "200 frames of pushing must not accumulate through the wall"
        )
    }

    func test_allFourWalls_blockMovement() {
        let (arView, resolver) = makeRoomScene()
        defer { _ = arView }

        let centre = SIMD3<Float>(0, 0, 0)
        let halfWidth = PlaceholderRoom.width / 2
        let halfDepth = PlaceholderRoom.depth / 2

        let left = resolver.resolve(from: centre, to: SIMD3<Float>(-20, 0, 0))
        let right = resolver.resolve(from: centre, to: SIMD3<Float>(20, 0, 0))
        let far = resolver.resolve(from: centre, to: SIMD3<Float>(0, 0, -20))
        let near = resolver.resolve(from: centre, to: SIMD3<Float>(0, 0, 20))

        XCTAssertGreaterThan(left.x, -halfWidth, "left wall must block")
        XCTAssertLessThan(right.x, halfWidth, "right wall must block")
        XCTAssertGreaterThan(far.z, -halfDepth, "far wall must block")
        XCTAssertLessThan(near.z, halfDepth, "near wall must block")
    }

    // MARK: - No-geometry fallback

    func test_unobstructedResolver_permitsEverything() {
        let resolver = UnobstructedCollisionResolver()
        let desired = SIMD3<Float>(100, 0, -100)

        XCTAssertEqual(resolver.resolve(from: .zero, to: desired), desired)
    }

    // MARK: - Camera collision (mechanism, now against real geometry)

    func test_thirdPersonCamera_doesNotClipThroughRoomWalls() {
        let (arView, _) = makeRoomScene()

        let camera = RealityKitCameraController(mode: .thirdPerson)
        let anchor = arView.scene.anchors.first as? AnchorEntity ?? AnchorEntity(world: .zero)
        camera.attach(to: anchor, in: arView.scene)

        camera.subject = MuseumCameraSubject(
            position: SIMD3<Float>(0, 0, -PlaceholderRoom.depth / 2 + 0.6),
            yaw: .pi
        )

        let target = camera.subject.eyePosition(eyeHeight: camera.configuration.eyeHeight)
        let desired = camera.desiredThirdPersonPosition()
        let resolved = camera.collisionResolvedPosition(from: target, to: desired)

        XCTAssertLessThan(
            length(resolved - target),
            length(desired - target),
            "the camera must be pulled in rather than sitting outside the room"
        )
    }

    // MARK: - Deterministic spawn (Navigation Principles)

    func test_spawnPoint_isInsideTheRoom_notMidSpace() {
        let spawn = PlaceholderRoom.spawnPoint

        XCTAssertLessThan(abs(spawn.position.x), PlaceholderRoom.width / 2, "spawn must be inside the room")
        XCTAssertLessThan(abs(spawn.position.z), PlaceholderRoom.depth / 2, "spawn must be inside the room")
        XCTAssertEqual(spawn.position.y, 0, accuracy: accuracy, "spawn must be on the floor, not floating")
        XCTAssertGreaterThan(spawn.position.z, 0, "spawn is at the near entrance, not the middle of the space")
    }

    func test_enteringRepeatedly_alwaysSpawnsAtTheSamePoint() {
        var controller = MuseumMovementController()

        for _ in 0..<3 {
            controller.teleport(to: PlaceholderRoom.spawnPoint)
            XCTAssertEqual(controller.subject, PlaceholderRoom.spawnPoint)

            for _ in 0..<120 {
                controller.update(input: MovementInput(forward: 1, yawDelta: 0.01), deltaTime: 1 / 60)
            }
            XCTAssertNotEqual(controller.subject, PlaceholderRoom.spawnPoint, "sanity: the viewer actually moved")
        }

        controller.teleport(to: PlaceholderRoom.spawnPoint)
        XCTAssertEqual(controller.subject, PlaceholderRoom.spawnPoint, "re-entry must return to the defined entrance")
    }

    func test_spawn_clearsMomentumFromAPreviousVisit() {
        var controller = MuseumMovementController()
        for _ in 0..<60 {
            controller.update(input: MovementInput(forward: 1), deltaTime: 1 / 60)
        }
        XCTAssertGreaterThan(length(controller.velocity), 0)

        controller.teleport(to: PlaceholderRoom.spawnPoint)

        XCTAssertEqual(length(controller.velocity), 0, accuracy: accuracy, "entering a room must not inherit momentum")
    }

    // MARK: - Placeholder room integrity

    func test_placeholderRoom_wallsAreCollidable() throws {
        let room = PlaceholderRoom.build()
        let collidableChildren = room.children.filter { $0.components[CollisionComponent.self] != nil }

        XCTAssertEqual(collidableChildren.count, 5, "every surface must carry a collision component, not just a mesh")
    }
}
