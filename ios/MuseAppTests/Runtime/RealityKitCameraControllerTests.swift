import RealityKit
import simd
import XCTest
@testable import MuseApp

@MainActor
final class RealityKitCameraControllerTests: XCTestCase {
    private let accuracy: Float = 0.0001

    // MARK: - Mode switching

    func test_defaultsToFirstPerson() {
        XCTAssertEqual(RealityKitCameraController().mode, .firstPerson)
    }

    func test_toggleMode_alternatesBetweenTheTwoSupportedModes() {
        let camera = RealityKitCameraController()

        camera.toggleMode()
        XCTAssertEqual(camera.mode, .thirdPerson)

        camera.toggleMode()
        XCTAssertEqual(camera.mode, .firstPerson)
    }

    func test_setMode_toCurrentMode_isANoOp() {
        let camera = RealityKitCameraController(mode: .thirdPerson)
        let before = camera.cameraEntity.transform.matrix

        camera.setMode(.thirdPerson)

        XCTAssertEqual(camera.cameraEntity.transform.matrix, before)
    }

    func test_switchingModes_neverMovesTheSubject() {
        let subject = MuseumCameraSubject(position: SIMD3<Float>(4, 0, -7), yaw: .pi / 3)
        let camera = RealityKitCameraController(subject: subject)

        for _ in 0..<10 {
            camera.toggleMode()
            XCTAssertEqual(camera.subject, subject, "a mode switch must never disturb the viewer's position or orientation")
        }
    }

    func test_switchingModesAndBack_restoresTheOriginalCameraTransform() {
        let camera = RealityKitCameraController(subject: MuseumCameraSubject(position: SIMD3<Float>(1, 0, 2), yaw: 0.75))
        let firstPersonTransform = camera.cameraEntity.transform.matrix

        camera.setMode(.thirdPerson)
        camera.setMode(.firstPerson)

        assertMatrix(camera.cameraEntity.transform.matrix, equals: firstPersonTransform)
    }

    // MARK: - First person

    func test_firstPerson_sitsAtEyeHeightOverTheSubject() {
        let configuration = MuseumCameraConfiguration(eyeHeight: 1.6)
        let camera = RealityKitCameraController(
            mode: .firstPerson,
            subject: MuseumCameraSubject(position: SIMD3<Float>(2, 0, 3)),
            configuration: configuration
        )

        let position = camera.cameraEntity.transform.translation
        XCTAssertEqual(position.x, 2, accuracy: accuracy)
        XCTAssertEqual(position.y, 1.6, accuracy: accuracy, "first person is locked to the subject's eye height")
        XCTAssertEqual(position.z, 3, accuracy: accuracy)
    }

    func test_firstPerson_facesWhereTheSubjectFaces() {
        let yaw: Float = .pi / 2
        let camera = RealityKitCameraController(mode: .firstPerson, subject: MuseumCameraSubject(yaw: yaw))

        let cameraForward = camera.cameraEntity.transform.rotation.act(SIMD3<Float>(0, 0, -1))
        let subjectForward = MuseumCameraSubject(yaw: yaw).forward

        XCTAssertEqual(cameraForward.x, subjectForward.x, accuracy: accuracy)
        XCTAssertEqual(cameraForward.y, subjectForward.y, accuracy: accuracy)
        XCTAssertEqual(cameraForward.z, subjectForward.z, accuracy: accuracy)
    }

    // MARK: - Third person

    func test_thirdPerson_sitsBehindAndAboveTheSubject() {
        let configuration = MuseumCameraConfiguration(eyeHeight: 1.6, thirdPersonDistance: 3, thirdPersonHeight: 0.8)
        let camera = RealityKitCameraController(
            mode: .thirdPerson,
            subject: MuseumCameraSubject(position: .zero, yaw: 0),
            configuration: configuration
        )

        let position = camera.cameraEntity.transform.translation
        XCTAssertEqual(position.x, 0, accuracy: accuracy)
        XCTAssertEqual(position.y, 1.6 + 0.8, accuracy: accuracy, "third person sits above the subject's eye line")
        XCTAssertEqual(position.z, 3, accuracy: accuracy, "third person trails behind the subject")
    }

    func test_thirdPerson_followsThroughTurns() {
        let configuration = MuseumCameraConfiguration(eyeHeight: 1.6, thirdPersonDistance: 3, thirdPersonHeight: 0)
        let subject = MuseumCameraSubject(position: .zero, yaw: .pi / 2)
        let camera = RealityKitCameraController(mode: .thirdPerson, subject: subject, configuration: configuration)

        XCTAssertEqual(subject.forward.x, -1, accuracy: 0.001, "sanity: yaw π/2 faces world -X")
        XCTAssertEqual(subject.forward.z, 0, accuracy: 0.001)

        let position = camera.cameraEntity.transform.translation
        XCTAssertEqual(position.x, 3, accuracy: 0.001, "behind a subject facing -X is +X")
        XCTAssertEqual(position.z, 0, accuracy: 0.001)
    }

    // MARK: - Camera collision (third person)

    func test_cameraCollision_withNoGeometry_keepsFullDistance() {
        let camera = RealityKitCameraController(mode: .thirdPerson)
        let target = camera.subject.eyePosition(eyeHeight: camera.configuration.eyeHeight)
        let desired = camera.desiredThirdPersonPosition()

        let resolved = camera.collisionResolvedPosition(from: target, to: desired)

        XCTAssertEqual(resolved.x, desired.x, accuracy: accuracy)
        XCTAssertEqual(resolved.y, desired.y, accuracy: accuracy)
        XCTAssertEqual(resolved.z, desired.z, accuracy: accuracy)
    }

    func test_cameraCollision_withGeometryBehindSubject_pullsCameraIn() throws {
        let arView = ARView(frame: CGRect(x: 0, y: 0, width: 100, height: 100), cameraMode: .nonAR, automaticallyConfigureSession: false)
        let anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)

        let configuration = MuseumCameraConfiguration(eyeHeight: 1.6, thirdPersonDistance: 3, thirdPersonHeight: 0, collisionPadding: 0.2)
        let camera = RealityKitCameraController(mode: .thirdPerson, configuration: configuration)
        camera.attach(to: anchor, in: arView.scene)

        let wall = ModelEntity(mesh: .generateBox(width: 4, height: 4, depth: 0.1))
        wall.position = SIMD3<Float>(0, 1.6, 1.0)
        wall.generateCollisionShapes(recursive: false)
        anchor.addChild(wall)

        let target = camera.subject.eyePosition(eyeHeight: configuration.eyeHeight)
        let desired = camera.desiredThirdPersonPosition()
        let resolved = camera.collisionResolvedPosition(from: target, to: desired)

        let resolvedDistance = length(resolved - target)
        XCTAssertLessThan(resolvedDistance, 3.0, "the camera must be pulled in rather than clipping through the wall")
        XCTAssertGreaterThan(resolvedDistance, 0, "the camera must not collapse onto the subject")
        XCTAssertLessThanOrEqual(resolvedDistance, 1.0, "the camera must stop at or before the obstructing surface")
    }

    // MARK: - Helpers

    private func assertMatrix(_ lhs: simd_float4x4, equals rhs: simd_float4x4, file: StaticString = #filePath, line: UInt = #line) {
        for column in 0..<4 {
            for row in 0..<4 {
                XCTAssertEqual(lhs[column][row], rhs[column][row], accuracy: accuracy, file: file, line: line)
            }
        }
    }
}
