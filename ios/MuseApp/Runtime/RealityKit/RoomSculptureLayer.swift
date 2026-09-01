import RealityKit
import simd
import UIKit

@MainActor
final class RoomSculptureLayer {
    let root = Entity()

    private struct Mount {
        let entity: ModelEntity
        var catalogID: String
        var transform: SlotTransform
    }

    private var mounts: [Int: Mount] = [:]
    private let slotTable: RoomVariantSlotTable
    private let models: any SculptureModelProviding

    init(slotTable: RoomVariantSlotTable, models: any SculptureModelProviding) {
        self.slotTable = slotTable
        self.models = models
        root.name = "RoomSculptureLayer"
    }

    // MARK: - Read-outs

    var mountedSlotIndices: Set<Int> { Set(mounts.keys) }

    func catalogID(atSlot slotIndex: Int) -> String? { mounts[slotIndex]?.catalogID }

    func position(atSlot slotIndex: Int) -> SIMD3<Float>? { mounts[slotIndex]?.entity.position }

    private(set) var unrenderableSlotIndices: Set<Int> = []

    // MARK: - Reconciliation

    func apply(_ sculptures: [SculptureInstance]) async {
        var seen: Set<Int> = []
        var unrenderable: Set<Int> = []

        for sculpture in RoomSculptures.sorted(sculptures) {
            seen.insert(sculpture.slotIndex)

            if let existing = mounts[sculpture.slotIndex], existing.catalogID == sculpture.catalogID {
                continue
            }
            guard let transform = slotTable.sculptureTransforms[sculpture.slotIndex] else {
                unrenderable.insert(sculpture.slotIndex)
                remove(slotIndex: sculpture.slotIndex)
                continue
            }
            guard let source = await models.modelSource(forCatalogID: sculpture.catalogID) else {
                unrenderable.insert(sculpture.slotIndex)
                remove(slotIndex: sculpture.slotIndex)
                continue
            }

            remove(slotIndex: sculpture.slotIndex)
            let entity = Self.makeEntity(for: source, catalogID: sculpture.catalogID, transform: transform)
            entity.name = "sculpture-\(sculpture.slotIndex)-\(sculpture.catalogID)"
            root.addChild(entity)
            mounts[sculpture.slotIndex] = Mount(entity: entity, catalogID: sculpture.catalogID, transform: transform)
        }

        for slotIndex in mounts.keys where !seen.contains(slotIndex) {
            remove(slotIndex: slotIndex)
        }
        unrenderableSlotIndices = unrenderable
    }

    func tearDown() {
        for slotIndex in mounts.keys {
            remove(slotIndex: slotIndex)
        }
        mounts.removeAll()
        unrenderableSlotIndices = []
    }

    private func remove(slotIndex: Int) {
        guard let mount = mounts.removeValue(forKey: slotIndex) else { return }
        mount.entity.model?.materials = []
        mount.entity.removeFromParent()
    }

    // MARK: - Geometry

    private static func makeEntity(for source: SculptureModelSource, catalogID: String, transform: SlotTransform) -> ModelEntity {
        let entity: ModelEntity
        switch source {
        case .verificationFixture:
            entity = fixtureEntity(envelope: transform.scale, catalogID: catalogID)
        }
        entity.transform = Transform(
            scale: .one,
            rotation: transform.rotation,
            translation: transform.position
        )
        entity.collision = nil
        return entity
    }

    private static func fixtureEntity(envelope: SIMD3<Float>, catalogID: String) -> ModelEntity {
        let height = max(envelope.y, 0.2)
        let footprint = max(min(envelope.x, envelope.z), 0.1)

        let plinthHeight = height * 0.35
        let plinth = ModelEntity(
            mesh: .generateBox(size: SIMD3<Float>(footprint, plinthHeight, footprint), cornerRadius: footprint * 0.05),
            materials: [fixtureMaterial(brightness: 0.55)]
        )
        plinth.position = SIMD3<Float>(0, plinthHeight / 2, 0)

        let bodyHeight = height - plinthHeight
        let body = ModelEntity(
            mesh: .generateBox(size: SIMD3<Float>(footprint * 0.55, bodyHeight, footprint * 0.55), cornerRadius: footprint * 0.08),
            materials: [fixtureMaterial(brightness: 0.72)]
        )
        body.position = SIMD3<Float>(0, plinthHeight + bodyHeight / 2, 0)

        let container = ModelEntity()
        container.addChild(plinth)
        container.addChild(body)
        return container
    }

    private static func fixtureMaterial(brightness: CGFloat) -> PhysicallyBasedMaterial {
        var material = PhysicallyBasedMaterial()
        material.baseColor = .init(tint: UIColor(white: brightness, alpha: 1))
        material.roughness = .init(floatLiteral: 0.85)
        material.metallic = .init(floatLiteral: 0)
        return material
    }
}
