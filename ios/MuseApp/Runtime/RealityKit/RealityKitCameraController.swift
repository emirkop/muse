import RealityKit
import simd

@MainActor
public final class RealityKitCameraController: MuseumCameraControlling {
    public private(set) var mode: MuseumCameraMode
    public var subject: MuseumCameraSubject {
        didSet { updateCameraTransform() }
    }

    public let configuration: MuseumCameraConfiguration

    let cameraEntity = PerspectiveCamera()

    private weak var scene: RealityKit.Scene?

    public private(set) var focusZoom: Float = 0
    public var isFocusZoomEnabled = true {
        didSet {
            guard !isFocusZoomEnabled, focusZoom != 0 else { return }
            focusZoom = 0
            applyFieldOfView()
        }
    }

    public init(
        mode: MuseumCameraMode = .firstPerson,
        subject: MuseumCameraSubject = MuseumCameraSubject(),
        configuration: MuseumCameraConfiguration = .default
    ) {
        self.mode = mode
        self.subject = subject
        self.configuration = configuration
        updateCameraTransform()
        applyFieldOfView()
    }

    func attach(to anchor: AnchorEntity, in scene: RealityKit.Scene?) {
        self.scene = scene
        anchor.addChild(cameraEntity)
        updateCameraTransform()
    }

    func detach() {
        cameraEntity.removeFromParent()
        scene = nil
    }

    public func setMode(_ mode: MuseumCameraMode) {
        guard mode != self.mode else { return }
        self.mode = mode
        smoothedDistance = nil
        updateCameraTransform()
    }

    public func toggleMode() {
        setMode(mode == .firstPerson ? .thirdPerson : .firstPerson)
    }

    // MARK: - Focus zoom

    public func advanceFocusZoom(towards active: Bool, deltaTime: Float) {
        let target: Float = (active && isFocusZoomEnabled && mode == .firstPerson) ? 1 : 0
        guard focusZoom != target else { return }

        if deltaTime <= 0 {
            return
        }
        let blend = min(configuration.focusZoomSmoothing * deltaTime, 1)
        focusZoom += (target - focusZoom) * blend
        if abs(target - focusZoom) < 0.001 { focusZoom = target }
        applyFieldOfView()
    }

    private func applyFieldOfView() {
        var camera = cameraEntity.camera
        camera.fieldOfViewInDegrees = currentFieldOfViewDegrees
        cameraEntity.camera = camera
    }

    var currentFieldOfViewDegrees: Float {
        let base = configuration.fieldOfViewDegrees
        let focused = configuration.focusedFieldOfViewDegrees
        return base + (focused - base) * focusZoom
    }

    // MARK: - Transform derivation

    private func updateCameraTransform() {
        switch mode {
        case .firstPerson:
            applyFirstPersonTransform()
        case .thirdPerson:
            applyThirdPersonTransform()
        }
    }

    private func applyFirstPersonTransform() {
        cameraEntity.transform = Transform(
            scale: .one,
            rotation: subject.orientation,
            translation: subject.eyePosition(eyeHeight: configuration.eyeHeight)
        )
    }

    // MARK: - Third person ( collision, hardened at )

    private var smoothedDistance: Float?
    private(set) var framingAmount: Float = 0

    public var thirdPersonDistance: Float {
        smoothedDistance ?? framedBoomLength
    }

    public func advanceThirdPersonFraming(towards active: Bool, deltaTime: Float) {
        guard deltaTime > 0 else { return }

        let target: Float = (active && mode == .thirdPerson) ? 1 : 0
        if framingAmount != target {
            let blend = min(configuration.thirdPersonFramingSmoothing * deltaTime, 1)
            framingAmount += (target - framingAmount) * blend
            if abs(target - framingAmount) < 0.001 { framingAmount = target }
        }

        if mode == .thirdPerson, let current = smoothedDistance {
            let allowed = resolvedBoomLength()
            if allowed > current {
                let blend = min(configuration.thirdPersonRecoverySmoothing * deltaTime, 1)
                var grown = current + (allowed - current) * blend
                if abs(allowed - grown) < 0.001 { grown = allowed }
                smoothedDistance = grown
            }
        }

        if mode == .thirdPerson { applyThirdPersonTransform() }
    }

    private func applyThirdPersonTransform() {
        let target = subject.eyePosition(eyeHeight: configuration.eyeHeight)
        let allowed = resolvedBoomLength()

        let length: Float
        if let current = smoothedDistance {
            length = min(current, allowed)
        } else {
            length = allowed
        }
        smoothedDistance = length

        let position = target + boomDirection() * length
        cameraEntity.look(at: target, from: position, relativeTo: nil)
    }

    func boomDirection() -> SIMD3<Float> {
        let localOffset = SIMD3<Float>(0, configuration.thirdPersonHeight, configuration.thirdPersonDistance)
        let rotated = subject.orientation.act(localOffset)
        let magnitude = simd.length(rotated)
        guard magnitude > .ulpOfOne else { return SIMD3<Float>(0, 0, 1) }
        return rotated / magnitude
    }

    var framedBoomLength: Float {
        let localOffset = SIMD3<Float>(0, configuration.thirdPersonHeight, configuration.thirdPersonDistance)
        let full = simd.length(localOffset)
        let framed = full * (1 - configuration.thirdPersonFramingFraction * framingAmount)
        return max(framed, configuration.thirdPersonMinimumDistance)
    }

    func desiredThirdPersonPosition() -> SIMD3<Float> {
        subject.eyePosition(eyeHeight: configuration.eyeHeight) + boomDirection() * framedBoomLength
    }

    func resolvedBoomLength() -> Float {
        let desired = framedBoomLength
        guard let scene else { return desired }

        let origin = subject.eyePosition(eyeHeight: configuration.eyeHeight)
        let direction = boomDirection()
        let probe = ShapeResource.generateSphere(radius: configuration.cameraProbeRadius)
        let identity = simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0))

        let hits = scene.convexCast(
            convexShape: probe,
            fromPosition: origin,
            fromOrientation: identity,
            toPosition: origin + direction * desired,
            toOrientation: identity,
            query: .nearest
        )
        guard let nearest = hits.first else { return desired }

        let pulledIn = nearest.distance - configuration.collisionPadding
        return min(desired, max(pulledIn, configuration.thirdPersonMinimumDistance))
    }

    func collisionResolvedPosition(from target: SIMD3<Float>, to desired: SIMD3<Float>) -> SIMD3<Float> {
        let offset = desired - target
        let distance = simd.length(offset)
        guard distance > .ulpOfOne else { return desired }
        return target + (offset / distance) * min(resolvedBoomLength(), distance)
    }
}
