import simd

public struct MuseumCollisionConfiguration: Equatable, Sendable {
    public var bodyRadius: Float
    public var sweepHeight: Float
    public var skinWidth: Float

    public init(bodyRadius: Float = 0.3, sweepHeight: Float = 0.9, skinWidth: Float = 0.02) {
        self.bodyRadius = bodyRadius
        self.sweepHeight = sweepHeight
        self.skinWidth = skinWidth
    }

    public static let `default` = MuseumCollisionConfiguration()
}

@MainActor
public protocol MovementCollisionResolving: AnyObject {
    func resolve(from current: SIMD3<Float>, to desired: SIMD3<Float>) -> SIMD3<Float>
}

@MainActor
public final class UnobstructedCollisionResolver: MovementCollisionResolving {
    public init() {}

    public func resolve(from current: SIMD3<Float>, to desired: SIMD3<Float>) -> SIMD3<Float> {
        desired
    }
}
