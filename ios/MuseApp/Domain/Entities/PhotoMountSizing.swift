import Foundation
import simd

public enum PhotoMountSizing {
    public struct PlaneSize: Equatable, Sendable {
        public let width: Float
        public let height: Float

        public init(width: Float, height: Float) {
            self.width = width
            self.height = height
        }
    }

    public static func planeSize(envelope: SlotTransform, pixelWidth: Int, pixelHeight: Int) -> PlaneSize {
        planeSize(
            envelopeWidth: envelope.scale.x,
            envelopeHeight: envelope.scale.y,
            pixelWidth: pixelWidth,
            pixelHeight: pixelHeight
        )
    }

    public static func planeSize(envelopeWidth: Float, envelopeHeight: Float, pixelWidth: Int, pixelHeight: Int) -> PlaneSize {
        guard envelopeWidth > 0, envelopeHeight > 0, pixelWidth > 0, pixelHeight > 0 else {
            return PlaneSize(width: 0, height: 0)
        }
        let aspect = Float(pixelWidth) / Float(pixelHeight)
        let envelopeAspect = envelopeWidth / envelopeHeight

        if aspect >= envelopeAspect {
            return PlaneSize(width: envelopeWidth, height: envelopeWidth / aspect)
        }
        return PlaneSize(width: envelopeHeight * aspect, height: envelopeHeight)
    }
}
