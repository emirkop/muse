import Foundation
import simd

public enum RoomPhotoFocus {
    public static let interimFocusRadiusMetres: Float = 2.6
    public static let interimReleaseRadiusMetres: Float = 3.3

    public static let interimFocusHalfAngleRadians: Float = 0.49
    public static let interimReleaseHalfAngleRadians: Float = 0.70

    public static let interimSwitchAlignmentMargin: Float = 0.04

    public static func focusedPhoto(
        eyePosition: SIMD3<Float>,
        forward: SIMD3<Float>,
        placements: [ResolvedPhotoPlacement],
        currentlyFocused: String? = nil
    ) -> FocusedPhoto? {
        let forwardLength = length(forward)
        guard forwardLength > .ulpOfOne else { return nil }
        let view = forward / forwardLength

        var best: FocusedPhoto?
        var incumbent: FocusedPhoto?

        for placement in placements {
            guard let candidate = evaluate(placement, eyePosition: eyePosition, view: view) else { continue }

            if placement.photoAssetID == currentlyFocused {
                if candidate.distance <= interimReleaseRadiusMetres,
                   candidate.alignment >= cos(interimReleaseHalfAngleRadians) {
                    incumbent = candidate
                }
            }

            guard candidate.distance <= interimFocusRadiusMetres,
                  candidate.alignment >= cos(interimFocusHalfAngleRadians) else { continue }
            if isBetter(candidate, than: best) {
                best = candidate
            }
        }

        if let incumbent {
            guard let best, best.photoAssetID != incumbent.photoAssetID else { return incumbent }
            return best.alignment > incumbent.alignment + interimSwitchAlignmentMargin ? best : incumbent
        }
        return best
    }

    private static func evaluate(
        _ placement: ResolvedPhotoPlacement,
        eyePosition: SIMD3<Float>,
        view: SIMD3<Float>
    ) -> FocusedPhoto? {
        let centre = placement.transform.position
        let toPhoto = centre - eyePosition
        let distance = length(toPhoto)
        guard distance > .ulpOfOne else { return nil }

        let normal = placement.transform.rotation.act(SIMD3<Float>(0, 0, 1))
        guard dot(normal, -toPhoto) > 0 else { return nil }

        return FocusedPhoto(
            photoAssetID: placement.photoAssetID,
            slotIndex: placement.slotIndex,
            distance: distance,
            alignment: dot(view, toPhoto / distance)
        )
    }

    private static func isBetter(_ candidate: FocusedPhoto, than current: FocusedPhoto?) -> Bool {
        guard let current else { return true }
        if candidate.alignment != current.alignment { return candidate.alignment > current.alignment }
        if candidate.distance != current.distance { return candidate.distance < current.distance }
        return candidate.slotIndex < current.slotIndex
    }
}

public struct FocusedPhoto: Equatable, Sendable {
    public let photoAssetID: String
    public let slotIndex: Int
    public let distance: Float
    public let alignment: Float

    public init(photoAssetID: String, slotIndex: Int, distance: Float, alignment: Float) {
        self.photoAssetID = photoAssetID
        self.slotIndex = slotIndex
        self.distance = distance
        self.alignment = alignment
    }
}
