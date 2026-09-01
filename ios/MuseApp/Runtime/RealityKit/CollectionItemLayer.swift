import RealityKit
import simd
import UIKit

@MainActor
final class CollectionItemLayer {
    let root = Entity()

    enum Appearance: Equatable {
        case developmentPlaceholder
    }

    private struct Mount {
        let entity: ModelEntity
        var slotIndex: Int
        var transform: SlotTransform
    }

    private var mounts: [String: Mount] = [:]
    private let appearance: Appearance

    init(appearance: Appearance = .developmentPlaceholder) {
        self.appearance = appearance
        root.name = "CollectionItemLayer"
    }

    // MARK: - Read-outs

    var mountedItemIDs: Set<String> { Set(mounts.keys) }

    func slotIndex(forItem itemID: String) -> Int? { mounts[itemID]?.slotIndex }

    func position(forItem itemID: String) -> SIMD3<Float>? { mounts[itemID]?.entity.position }

    func mountedItemPositions() -> [(itemID: String, worldPosition: SIMD3<Float>)] {
        mounts.map { ($0.key, $0.value.entity.position(relativeTo: nil)) }
    }

    func itemID(atSlot slotIndex: Int) -> String? {
        mounts.first { $0.value.slotIndex == slotIndex }?.key
    }

    // MARK: - Reconciliation

    func apply(_ placements: [(item: CollectionItem, slot: CollectionItemSlot)]) {
        var seen: Set<String> = []

        for placement in placements {
            seen.insert(placement.item.id)

            if var existing = mounts[placement.item.id] {
                if existing.slotIndex != placement.slot.slotIndex || existing.transform != placement.slot.transform {
                    Self.position(existing.entity, at: placement.slot.transform)
                    existing.slotIndex = placement.slot.slotIndex
                    existing.transform = placement.slot.transform
                    mounts[placement.item.id] = existing
                }
                continue
            }

            let entity = Self.makeEntity(appearance: appearance, envelope: placement.slot.transform.scale)
            entity.name = "collection-item-\(placement.item.id)"
            Self.position(entity, at: placement.slot.transform)
            root.addChild(entity)
            mounts[placement.item.id] = Mount(
                entity: entity,
                slotIndex: placement.slot.slotIndex,
                transform: placement.slot.transform
            )
        }

        for itemID in mounts.keys where !seen.contains(itemID) {
            remove(itemID: itemID)
        }
        if let lifted = liftedItemID, !seen.contains(lifted) { liftedItemID = nil }
        if let target = targetItemID, !seen.contains(target) { targetItemID = nil }
        applyFeedback()
    }

    func tearDown() {
        for itemID in mounts.keys {
            remove(itemID: itemID)
        }
        mounts.removeAll()
        liftedItemID = nil
        targetItemID = nil
    }

    private func remove(itemID: String) {
        guard let mount = mounts.removeValue(forKey: itemID) else { return }
        mount.entity.model?.materials = []
        mount.entity.removeFromParent()
    }

    // MARK: - Interaction feedback

    private(set) var liftedItemID: String?
    private(set) var targetItemID: String?
    private(set) var targetEmptySlotIndex: Int?

    private static let liftScale: Float = 1.14
    private static let targetScale: Float = 1.07
    private static let restScale: Float = 1.0

    func setLifted(itemID: String?) {
        liftedItemID = itemID
        applyFeedback()
    }

    func setTarget(itemID: String?, emptySlotIndex: Int?) {
        targetItemID = itemID
        targetEmptySlotIndex = emptySlotIndex
        applyFeedback()
    }

    func clearInteractionFeedback() {
        liftedItemID = nil
        targetItemID = nil
        targetEmptySlotIndex = nil
        applyFeedback()
    }

    private func applyFeedback() {
        for (itemID, mount) in mounts {
            let scale: Float
            switch itemID {
            case liftedItemID: scale = Self.liftScale
            case targetItemID: scale = Self.targetScale
            default: scale = Self.restScale
            }
            mount.entity.scale = SIMD3<Float>(repeating: scale)
        }
    }

    // MARK: - Geometry

    private static func position(_ entity: ModelEntity, at transform: SlotTransform) {
        entity.transform = Transform(
            scale: .one,
            rotation: transform.rotation,
            translation: transform.position
        )
        entity.collision = nil
    }

    private static func makeEntity(appearance: Appearance, envelope: SIMD3<Float>) -> ModelEntity {
        switch appearance {
        case .developmentPlaceholder:
            return placeholderEntity(envelope: envelope)
        }
    }

    private static func placeholderEntity(envelope: SIMD3<Float>) -> ModelEntity {
        let height = max(envelope.y, 0.15) * 0.7
        let footprint = max(min(envelope.x, envelope.z), 0.08) * 0.7

        let baseHeight = max(height * 0.2, 0.01)
        let base = ModelEntity(
            mesh: .generateBox(size: SIMD3<Float>(footprint, baseHeight, footprint), cornerRadius: footprint * 0.08),
            materials: [placeholderMaterial(brightness: 0.5)]
        )
        base.position = SIMD3<Float>(0, baseHeight / 2, 0)

        let bodyHeight = max(height - baseHeight, 0.01)
        let body = ModelEntity(
            mesh: .generateBox(
                size: SIMD3<Float>(footprint * 0.6, bodyHeight, footprint * 0.6),
                cornerRadius: footprint * 0.12
            ),
            materials: [placeholderMaterial(brightness: 0.74)]
        )
        body.position = SIMD3<Float>(0, baseHeight + bodyHeight / 2, 0)

        let container = ModelEntity()
        container.addChild(base)
        container.addChild(body)
        return container
    }

    private static func placeholderMaterial(brightness: CGFloat) -> PhysicallyBasedMaterial {
        var material = PhysicallyBasedMaterial()
        material.baseColor = .init(tint: UIColor(white: brightness, alpha: 1))
        material.roughness = .init(floatLiteral: 0.8)
        material.metallic = .init(floatLiteral: 0)
        return material
    }
}
