import simd
import XCTest
@testable import MuseApp

final class MuseumMovementControllerTests: XCTestCase {
    private let accuracy: Float = 0.001

    private func run(
        _ controller: inout MuseumMovementController,
        input: MovementInput,
        seconds: Float,
        frameRate: Float = 60
    ) {
        let step = 1 / frameRate
        for _ in 0..<Int(seconds * frameRate) {
            controller.update(input: input, deltaTime: step)
        }
    }

    // MARK: - Translation

    func test_forwardInput_movesAlongFacingDirection() {
        var controller = MuseumMovementController()

        run(&controller, input: MovementInput(forward: 1), seconds: 1)

        XCTAssertLessThan(controller.subject.position.z, -0.5, "forward input must advance along the facing direction")
        XCTAssertEqual(controller.subject.position.x, 0, accuracy: accuracy)
        XCTAssertEqual(controller.subject.position.y, 0, accuracy: accuracy, "movement stays on the ground plane")
    }

    func test_backwardInput_movesOppositeFacingDirection() {
        var controller = MuseumMovementController()

        run(&controller, input: MovementInput(forward: -1), seconds: 1)

        XCTAssertGreaterThan(controller.subject.position.z, 0.5)
    }

    func test_strafeInput_movesSideways_withoutTurning() {
        var controller = MuseumMovementController()

        run(&controller, input: MovementInput(strafe: 1), seconds: 1)

        XCTAssertGreaterThan(controller.subject.position.x, 0.5, "strafing right moves along +X when facing -Z")
        XCTAssertEqual(controller.subject.yaw, 0, accuracy: accuracy, "strafing must not change facing")
    }

    func test_movementFollowsFacing_afterTurning() {
        var controller = MuseumMovementController()

        controller.update(input: MovementInput(yawDelta: .pi / 2), deltaTime: 1 / 60)
        run(&controller, input: MovementInput(forward: 1), seconds: 1)

        XCTAssertLessThan(controller.subject.position.x, -0.5, "after turning, forward must follow the new heading")
        XCTAssertEqual(controller.subject.position.z, 0, accuracy: 0.05)
    }

    // MARK: - Input normalisation

    func test_diagonalInput_isNotFasterThanStraightInput() {
        var straight = MuseumMovementController()
        var diagonal = MuseumMovementController()

        run(&straight, input: MovementInput(forward: 1), seconds: 2)
        run(&diagonal, input: MovementInput(forward: 1, strafe: 1), seconds: 2)

        let straightDistance = length(straight.subject.position)
        let diagonalDistance = length(diagonal.subject.position)
        XCTAssertEqual(diagonalDistance, straightDistance, accuracy: 0.01, "diagonal movement must not outpace straight movement")
    }

    func test_outOfRangeInput_isClamped() {
        var clamped = MuseumMovementController()
        var maximum = MuseumMovementController()

        run(&clamped, input: MovementInput(forward: 50), seconds: 2)
        run(&maximum, input: MovementInput(forward: 1), seconds: 2)

        XCTAssertEqual(
            length(clamped.subject.position),
            length(maximum.subject.position),
            accuracy: 0.01,
            "input beyond full deflection must not produce extra speed"
        )
    }

    // MARK: - Frame-rate independence

    func test_distanceTravelled_isIndependentOfFrameRate() {
        var slow = MuseumMovementController()
        var fast = MuseumMovementController()

        run(&slow, input: MovementInput(forward: 1), seconds: 3, frameRate: 30)
        run(&fast, input: MovementInput(forward: 1), seconds: 3, frameRate: 120)

        XCTAssertEqual(length(slow.subject.position), length(fast.subject.position), accuracy: 0.05)
    }

    func test_zeroOrNegativeDeltaTime_isIgnored() {
        var controller = MuseumMovementController()

        controller.update(input: MovementInput(forward: 1), deltaTime: 0)
        controller.update(input: MovementInput(forward: 1), deltaTime: -1)

        XCTAssertEqual(controller.subject.position, .zero)
    }

    // MARK: - Smoothing

    func test_velocityRampsUpRatherThanJumping() {
        var controller = MuseumMovementController()

        controller.update(input: MovementInput(forward: 1), deltaTime: 1 / 60)
        let afterOneFrame = length(controller.velocity)

        run(&controller, input: MovementInput(forward: 1), seconds: 1)
        let atSteadyState = length(controller.velocity)

        XCTAssertGreaterThan(afterOneFrame, 0, "movement must begin immediately, not after a delay")
        XCTAssertLessThan(afterOneFrame, atSteadyState * 0.5, "velocity must ramp in rather than snapping to full speed")
    }

    func test_releasingInput_decelerates_ratherThanStoppingDead() {
        var controller = MuseumMovementController()
        run(&controller, input: MovementInput(forward: 1), seconds: 1)

        controller.update(input: .idle, deltaTime: 1 / 60)
        let justAfterRelease = length(controller.velocity)

        run(&controller, input: .idle, seconds: 1)
        let later = length(controller.velocity)

        XCTAssertGreaterThan(justAfterRelease, 0, "momentum must decay rather than cutting to zero")
        XCTAssertLessThan(later, 0.01, "movement must actually come to rest")
    }

    // MARK: - Teleport

    func test_teleport_setsPositionAndClearsMomentum() {
        var controller = MuseumMovementController()
        run(&controller, input: MovementInput(forward: 1), seconds: 1)
        XCTAssertGreaterThan(length(controller.velocity), 0)

        let spawn = MuseumCameraSubject(position: SIMD3<Float>(5, 0, -2), yaw: .pi)
        controller.teleport(to: spawn)

        XCTAssertEqual(controller.subject, spawn)
        XCTAssertEqual(length(controller.velocity), 0, accuracy: accuracy, "a teleport must not carry momentum into the new position")
    }

    // MARK: - MovementInput

    func test_idleInput_reportsIdle() {
        XCTAssertTrue(MovementInput.idle.isIdle)
        XCTAssertFalse(MovementInput(forward: 0.1).isIdle)
        XCTAssertFalse(MovementInput(yawDelta: 0.1).isIdle)
    }
}
