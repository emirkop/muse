import RealityKit
import UIKit

@MainActor
enum LobbyCardScreenHitTester {
    static let defaultRadius: CGFloat = 110

    static func nearestRoomID(
        to point: CGPoint,
        in layer: LobbyCardLayer,
        projectedBy arView: ARView,
        radius: CGFloat = defaultRadius
    ) -> String? {
        var bestRoomID: String?
        var bestDistance = CGFloat.greatestFiniteMagnitude
        let acceptable = arView.bounds.insetBy(dx: -radius, dy: -radius)

        for card in layer.mountedCardPositions() {
            guard let projected = arView.project(card.worldPosition),
                  acceptable.contains(projected) else { continue }
            let distance = hypot(projected.x - point.x, projected.y - point.y)
            guard distance <= radius, distance < bestDistance else { continue }
            bestDistance = distance
            bestRoomID = card.roomID
        }
        return bestRoomID
    }
}
