import RealityKit
import UIKit

@MainActor
enum CollectionItemScreenHitTester {
    static let defaultRadius: CGFloat = 70

    enum Hit: Equatable {
        case item(itemID: String)
        case emptySlot(slotIndex: Int)
    }

    static func nearestTarget(
        to point: CGPoint,
        in layer: CollectionItemLayer,
        emptySlots: [CollectionItemSlot],
        projectedBy arView: ARView,
        radius: CGFloat = defaultRadius,
        excludingItemID excluded: String? = nil
    ) -> Hit? {
        let acceptable = arView.bounds.insetBy(dx: -radius, dy: -radius)

        var bestItemID: String?
        var bestItemDistance = CGFloat.greatestFiniteMagnitude
        for mounted in layer.mountedItemPositions() where mounted.itemID != excluded {
            guard let projected = arView.project(mounted.worldPosition),
                  acceptable.contains(projected) else { continue }
            let distance = hypot(projected.x - point.x, projected.y - point.y)
            guard distance <= radius, distance < bestItemDistance else { continue }
            bestItemDistance = distance
            bestItemID = mounted.itemID
        }
        if let bestItemID {
            return .item(itemID: bestItemID)
        }

        var bestSlot: Int?
        var bestSlotDistance = CGFloat.greatestFiniteMagnitude
        for slot in emptySlots {
            guard let projected = arView.project(slot.transform.position),
                  acceptable.contains(projected) else { continue }
            let distance = hypot(projected.x - point.x, projected.y - point.y)
            guard distance <= radius, distance < bestSlotDistance else { continue }
            bestSlotDistance = distance
            bestSlot = slot.slotIndex
        }
        if let bestSlot {
            return .emptySlot(slotIndex: bestSlot)
        }
        return nil
    }
}
