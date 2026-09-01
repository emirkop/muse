import RealityKit
import UIKit

@MainActor
enum RoomPhotoScreenHitTester {
    static let defaultRadius: CGFloat = 90

    static func nearestPhotoAssetID(
        to point: CGPoint,
        in layer: RoomPhotoLayer,
        projectedBy arView: ARView,
        radius: CGFloat = defaultRadius,
        excluding excluded: String? = nil
    ) -> String? {
        var bestAssetID: String?
        var bestDistance = CGFloat.greatestFiniteMagnitude
        let acceptable = arView.bounds.insetBy(dx: -radius, dy: -radius)

        for photo in layer.mountedPhotoPositions() where photo.assetID != excluded {
            guard let projected = arView.project(photo.worldPosition),
                  acceptable.contains(projected) else { continue }
            let distance = hypot(projected.x - point.x, projected.y - point.y)
            guard distance <= radius, distance < bestDistance else { continue }
            bestDistance = distance
            bestAssetID = photo.assetID
        }
        return bestAssetID
    }
}
