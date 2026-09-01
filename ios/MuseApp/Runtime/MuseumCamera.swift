import simd

public enum MuseumCameraMode: Equatable, Sendable, CaseIterable {
    case firstPerson
    case thirdPerson
}

public struct MuseumCameraSubject: Equatable, Sendable {
    public var position: SIMD3<Float>
    public var yaw: Float

    public init(position: SIMD3<Float> = .zero, yaw: Float = 0) {
        self.position = position
        self.yaw = yaw
    }

    public var orientation: simd_quatf {
        simd_quatf(angle: yaw, axis: SIMD3<Float>(0, 1, 0))
    }

    public var forward: SIMD3<Float> {
        orientation.act(SIMD3<Float>(0, 0, -1))
    }

    public func eyePosition(eyeHeight: Float) -> SIMD3<Float> {
        position + SIMD3<Float>(0, eyeHeight, 0)
    }
}

public struct MuseumCameraConfiguration: Equatable, Sendable {
    public var eyeHeight: Float
    public var thirdPersonDistance: Float
    public var thirdPersonHeight: Float
    public var collisionPadding: Float
    public var fieldOfViewDegrees: Float
    public var focusedFieldOfViewDegrees: Float
    public var focusZoomSmoothing: Float

    // MARK: Third person

    public var thirdPersonMinimumDistance: Float
    public var cameraProbeRadius: Float
    public var thirdPersonRecoverySmoothing: Float
    public var thirdPersonFramingFraction: Float
    public var thirdPersonFramingSmoothing: Float

    public init(
        eyeHeight: Float = 1.6,
        thirdPersonDistance: Float = 3.0,
        thirdPersonHeight: Float = 0.8,
        collisionPadding: Float = 0.2,
        fieldOfViewDegrees: Float = 60,
        focusedFieldOfViewDegrees: Float = 46,
        focusZoomSmoothing: Float = 7,
        thirdPersonMinimumDistance: Float = 0.85,
        cameraProbeRadius: Float = 0.18,
        thirdPersonRecoverySmoothing: Float = 4,
        thirdPersonFramingFraction: Float = 0.45,
        thirdPersonFramingSmoothing: Float = 6
    ) {
        self.eyeHeight = eyeHeight
        self.thirdPersonDistance = thirdPersonDistance
        self.thirdPersonHeight = thirdPersonHeight
        self.collisionPadding = collisionPadding
        self.fieldOfViewDegrees = fieldOfViewDegrees
        self.focusedFieldOfViewDegrees = focusedFieldOfViewDegrees
        self.focusZoomSmoothing = focusZoomSmoothing
        self.thirdPersonMinimumDistance = thirdPersonMinimumDistance
        self.cameraProbeRadius = cameraProbeRadius
        self.thirdPersonRecoverySmoothing = thirdPersonRecoverySmoothing
        self.thirdPersonFramingFraction = thirdPersonFramingFraction
        self.thirdPersonFramingSmoothing = thirdPersonFramingSmoothing
    }

    public static let `default` = MuseumCameraConfiguration()
}

@MainActor
public protocol MuseumCameraControlling: AnyObject {
    var mode: MuseumCameraMode { get }
    var subject: MuseumCameraSubject { get set }

    func setMode(_ mode: MuseumCameraMode)
    func toggleMode()

    var focusZoom: Float { get }
    var isFocusZoomEnabled: Bool { get set }

    func advanceFocusZoom(towards active: Bool, deltaTime: Float)

    var thirdPersonDistance: Float { get }

    func advanceThirdPersonFraming(towards active: Bool, deltaTime: Float)
}
