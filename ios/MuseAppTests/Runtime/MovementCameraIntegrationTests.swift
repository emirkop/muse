import simd
import XCTest
@testable import MuseApp

@MainActor
final class MovementCameraIntegrationTests: XCTestCase {
    private let accuracy: Float = 0.0001

    private func walk(_ controller: inout MuseumMovementController, seconds: Float = 1) {
        let step: Float = 1 / 60
        for _ in 0..<Int(seconds * 60) {
            controller.update(input: MovementInput(forward: 1, yawDelta: 0.002), deltaTime: step)
        }
    }

    func test_identicalMovement_producesIdenticalSubject_inBothCameraModes() {
        var firstPersonRun = MuseumMovementController()
        var thirdPersonRun = MuseumMovementController()

        let firstPersonCamera = RealityKitCameraController(mode: .firstPerson)
        let thirdPersonCamera = RealityKitCameraController(mode: .thirdPerson)

        walk(&firstPersonRun)
        walk(&thirdPersonRun)
        firstPersonCamera.subject = firstPersonRun.subject
        thirdPersonCamera.subject = thirdPersonRun.subject

        XCTAssertEqual(
            firstPersonCamera.subject,
            thirdPersonCamera.subject,
            "the same movement must produce the same viewer state regardless of camera mode"
        )
    }

    func test_sameSubject_placesTheTwoCamerasDifferently() {
        var controller = MuseumMovementController()
        walk(&controller)

        let firstPerson = RealityKitCameraController(mode: .firstPerson)
        let thirdPerson = RealityKitCameraController(mode: .thirdPerson)
        firstPerson.subject = controller.subject
        thirdPerson.subject = controller.subject

        let firstPersonPosition = firstPerson.cameraEntity.transform.translation
        let thirdPersonPosition = thirdPerson.cameraEntity.transform.translation

        XCTAssertGreaterThan(
            length(thirdPersonPosition - firstPersonPosition),
            1,
            "the two modes must genuinely differ in where the camera sits, even from identical viewer state"
        )
    }

    func test_switchingModeMidMovement_doesNotDisturbTheViewer() {
        var controller = MuseumMovementController()
        let camera = RealityKitCameraController(mode: .firstPerson)

        walk(&controller)
        camera.subject = controller.subject
        let beforeSwitch = camera.subject

        camera.toggleMode()

        XCTAssertEqual(camera.subject, beforeSwitch, "switching camera mode mid-walk must not move the viewer")

        walk(&controller)
        camera.subject = controller.subject
        XCTAssertNotEqual(camera.subject, beforeSwitch, "movement must continue normally after a mode switch")
    }

    func test_firstPersonCamera_tracksMovingSubject() {
        var controller = MuseumMovementController()
        let camera = RealityKitCameraController(mode: .firstPerson)

        for _ in 0..<60 {
            controller.update(input: MovementInput(forward: 1), deltaTime: 1 / 60)
            camera.subject = controller.subject
        }

        let expected = controller.subject.eyePosition(eyeHeight: camera.configuration.eyeHeight)
        let actual = camera.cameraEntity.transform.translation
        XCTAssertEqual(actual.x, expected.x, accuracy: accuracy)
        XCTAssertEqual(actual.y, expected.y, accuracy: accuracy)
        XCTAssertEqual(actual.z, expected.z, accuracy: accuracy)
    }
}
