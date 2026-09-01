import simd

public enum MovementControlScheme: Equatable, Sendable, CaseIterable {
    case gesture
    case assistive
}

public struct MovementInput: Equatable, Sendable {
    public var forward: Float
    public var strafe: Float
    public var yawDelta: Float

    public init(forward: Float = 0, strafe: Float = 0, yawDelta: Float = 0) {
        self.forward = forward
        self.strafe = strafe
        self.yawDelta = yawDelta
    }

    public static let idle = MovementInput()

    public var normalised: MovementInput {
        var clamped = MovementInput(
            forward: min(max(forward, -1), 1),
            strafe: min(max(strafe, -1), 1),
            yawDelta: yawDelta
        )
        let magnitude = (clamped.forward * clamped.forward + clamped.strafe * clamped.strafe).squareRoot()
        if magnitude > 1 {
            clamped.forward /= magnitude
            clamped.strafe /= magnitude
        }
        return clamped
    }

    public var isIdle: Bool {
        forward == 0 && strafe == 0 && yawDelta == 0
    }
}

public struct MuseumMovementConfiguration: Equatable, Sendable {
    public var walkSpeed: Float
    public var turnSpeed: Float
    public var lookSensitivity: Float
    public var smoothing: Float

    public init(
        walkSpeed: Float = 1.4,
        turnSpeed: Float = 1.6,
        lookSensitivity: Float = 0.005,
        smoothing: Float = 12
    ) {
        self.walkSpeed = walkSpeed
        self.turnSpeed = turnSpeed
        self.lookSensitivity = lookSensitivity
        self.smoothing = smoothing
    }

    public static let `default` = MuseumMovementConfiguration()
}

public struct MuseumMovementController: Equatable, Sendable {
    public private(set) var subject: MuseumCameraSubject
    public var configuration: MuseumMovementConfiguration
    public private(set) var velocity: SIMD2<Float>

    public init(
        subject: MuseumCameraSubject = MuseumCameraSubject(),
        configuration: MuseumMovementConfiguration = .default
    ) {
        self.subject = subject
        self.configuration = configuration
        self.velocity = .zero
    }

    public mutating func update(input: MovementInput, deltaTime: Float) {
        guard deltaTime > 0 else { return }

        let intent = input.normalised
        subject.yaw += intent.yawDelta

        let requested = SIMD2<Float>(intent.strafe, intent.forward) * configuration.walkSpeed
        let blend = min(configuration.smoothing * deltaTime, 1)
        velocity += (requested - velocity) * blend

        let displacement = velocity * deltaTime
        let right = subject.orientation.act(SIMD3<Float>(1, 0, 0))
        let forward = subject.forward
        subject.position += right * displacement.x + forward * displacement.y
    }

    public mutating func teleport(to subject: MuseumCameraSubject) {
        self.subject = subject
        self.velocity = .zero
    }

    public mutating func applyResolvedPosition(_ position: SIMD3<Float>) {
        subject.position = position
    }
}
