import Foundation
import simd

public enum LobbyCardFocus {
    public static let interimFocusRadiusMetres: Float = 3.0

    public static func focusedCard(
        viewerPosition: SIMD3<Float>,
        placements: [ResolvedLobbyCardPlacement],
        radius: Float = interimFocusRadiusMetres
    ) -> ResolvedLobbyCardPlacement? {
        var best: ResolvedLobbyCardPlacement?
        var bestDistance = Float.greatestFiniteMagnitude

        for placement in placements {
            let distance = horizontalDistance(from: viewerPosition, to: placement.transform.position)
            guard distance <= radius else { continue }
            if distance < bestDistance || (distance == bestDistance && placement.cardIndex < (best?.cardIndex ?? Int.max)) {
                bestDistance = distance
                best = placement
            }
        }
        return best
    }

    static func horizontalDistance(from a: SIMD3<Float>, to b: SIMD3<Float>) -> Float {
        let dx = a.x - b.x
        let dz = a.z - b.z
        return (dx * dx + dz * dz).squareRoot()
    }
}
