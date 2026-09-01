import RealityKit
import simd

@MainActor
public final class RealityKitCollisionResolver: MovementCollisionResolving {
    private weak var scene: RealityKit.Scene?
    private let configuration: MuseumCollisionConfiguration
    private let bodyShape: ShapeResource

    public init(scene: RealityKit.Scene?, configuration: MuseumCollisionConfiguration = .default) {
        self.scene = scene
        self.configuration = configuration
        self.bodyShape = .generateSphere(radius: configuration.bodyRadius)
    }

    public func resolve(from current: SIMD3<Float>, to desired: SIMD3<Float>) -> SIMD3<Float> {
        guard scene != nil else { return desired }

        let attempted = desired - current
        guard length(attempted) > .ulpOfOne else { return current }

        if let blocked = firstBlockingHit(from: current, movement: attempted) {
            let normal = normalize(blocked.normal)
            let slide = attempted - normal * dot(attempted, normal)

            if length(slide) > .ulpOfOne, firstBlockingHit(from: current, movement: slide) == nil {
                return current + slide
            }
            return current
        }

        return current + attempted
    }

    private func firstBlockingHit(from origin: SIMD3<Float>, movement: SIMD3<Float>) -> CollisionCastHit? {
        guard let scene else { return nil }

        let sweepOffset = SIMD3<Float>(0, configuration.sweepHeight, 0)
        let from = origin + sweepOffset
        let to = from + movement + normalize(movement) * configuration.skinWidth

        let hits = scene.convexCast(
            convexShape: bodyShape,
            fromPosition: from,
            fromOrientation: simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0)),
            toPosition: to,
            toOrientation: simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0)),
            query: .nearest
        )
        return hits.first
    }
}
