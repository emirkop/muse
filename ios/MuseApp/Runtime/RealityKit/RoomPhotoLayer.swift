import RealityKit
import simd
import UIKit

@MainActor
final class RoomPhotoLayer {
    let root = Entity()

    private(set) var generation = 0
    private var mounts: [String: PhotoMount] = [:]
    private var assetBySlotAtMount: [Int: String] = [:]

    struct PhotoMount {
        let entity: ModelEntity
        var placement: ResolvedPhotoPlacement
        var isTextured: Bool
        var pixelWidth: Int
        var pixelHeight: Int
        var hasFailed: Bool
    }

    init() {
        root.name = "RoomPhotoLayer"
    }

    static let provisionalAspect = (width: 3, height: 2)

    // MARK: - Read-outs

    var mountedAssetIDs: Set<String> { Set(mounts.keys) }
    var texturedAssetIDs: Set<String> { Set(mounts.filter { $0.value.isTextured }.map(\.key)) }
    var failedAssetIDs: Set<String> { Set(mounts.filter { $0.value.hasFailed }.map(\.key)) }

    var mountedSlotIndices: Set<Int> { Set(mounts.values.map(\.placement.slotIndex)) }
    var texturedSlotIndices: Set<Int> { Set(mounts.values.filter(\.isTextured).map(\.placement.slotIndex)) }

    func slotIndex(forAsset assetID: String) -> Int? { mounts[assetID]?.placement.slotIndex }

    func assetID(atSlot slotIndex: Int) -> String? {
        mounts.first { $0.value.placement.slotIndex == slotIndex }?.key
    }

    func isTextured(assetID: String) -> Bool { mounts[assetID]?.isTextured ?? false }

    func mountedPhotoPositions() -> [(assetID: String, slotIndex: Int, worldPosition: SIMD3<Float>)] {
        mounts.map { ($0.key, $0.value.placement.slotIndex, $0.value.entity.position(relativeTo: nil)) }
    }

    func planeBounds(forAsset assetID: String) -> SIMD3<Float>? {
        mounts[assetID]?.entity.model?.mesh.bounds.extents
    }

    func planeBounds(forSlot slotIndex: Int) -> SIMD3<Float>? {
        assetID(atSlot: slotIndex).flatMap { planeBounds(forAsset: $0) }
    }

    func transform(forAsset assetID: String) -> SlotTransform? { mounts[assetID]?.placement.transform }

    // MARK: - Mounting

    @discardableResult
    func mount(_ placements: [ResolvedPhotoPlacement]) -> Int {
        tearDown()
        generation += 1

        for placement in placements {
            let size = PhotoMountSizing.planeSize(
                envelope: placement.transform,
                pixelWidth: Self.provisionalAspect.width,
                pixelHeight: Self.provisionalAspect.height
            )
            guard size.width > 0, size.height > 0 else { continue }

            let entity = ModelEntity(
                mesh: .generatePlane(width: size.width, height: size.height),
                materials: [Self.placeholderMaterial()]
            )
            entity.name = "photo-\(placement.photoAssetID)"
            entity.transform = Transform(
                scale: .one,
                rotation: placement.transform.rotation,
                translation: placement.transform.position
            )
            root.addChild(entity)
            mounts[placement.photoAssetID] = PhotoMount(
                entity: entity,
                placement: placement,
                isTextured: false,
                pixelWidth: Self.provisionalAspect.width,
                pixelHeight: Self.provisionalAspect.height,
                hasFailed: false
            )
            assetBySlotAtMount[placement.slotIndex] = placement.photoAssetID
        }
        return generation
    }

    func relayout(_ placements: [ResolvedPhotoPlacement]) {
        for placement in placements {
            guard var mount = mounts[placement.photoAssetID] else { continue }
            mount.placement = placement
            mount.entity.transform = Transform(
                scale: .one,
                rotation: placement.transform.rotation,
                translation: placement.transform.position
            )
            refit(&mount)
            mounts[placement.photoAssetID] = mount
        }
    }

    func setStoredDimensions(pixelWidth: Int, pixelHeight: Int, forAsset assetID: String, generation: Int) {
        guard generation == self.generation else { return }
        updateMount(assetID) { mount in
            guard !mount.isTextured else { return }
            mount.pixelWidth = pixelWidth
            mount.pixelHeight = pixelHeight
            refit(&mount)
        }
    }

    func apply(_ image: DecodedPhotoImage, forAsset assetID: String, generation: Int) async {
        guard generation == self.generation, hasMount(assetID) else { return }

        let texture: TextureResource
        do {
            texture = try await TextureResource(
                image: image.image,
                withName: "photo-\(assetID)",
                options: .init(semantic: .color, mipmapsMode: .allocateAndGenerateAll)
            )
        } catch {
            return
        }
        guard generation == self.generation else { return }
        updateMount(assetID) { mount in
            mount.pixelWidth = image.pixelWidth
            mount.pixelHeight = image.pixelHeight
            mount.isTextured = true
            mount.hasFailed = false
            mount.entity.model?.materials = [Self.photoMaterial(texture)]
            refit(&mount)
        }
    }

    func markFailed(assetID: String, generation: Int) {
        guard generation == self.generation else { return }
        updateMount(assetID) { $0.hasFailed = true }
    }

    private func hasMount(_ assetID: String) -> Bool {
        mounts[assetID] != nil || pendingRemovals[assetID] != nil
    }

    private func updateMount(_ assetID: String, _ update: (inout PhotoMount) -> Void) {
        if var mount = mounts[assetID] {
            update(&mount)
            mounts[assetID] = mount
        } else if var mount = pendingRemovals[assetID] {
            update(&mount)
            pendingRemovals[assetID] = mount
        }
    }

    func loadingAssetID(forSlot slotIndex: Int) -> String? { assetBySlotAtMount[slotIndex] }

    func tearDown() {
        clearInteractionFeedback()
        focusedAssetID = nil
        for mount in mounts.values {
            mount.entity.model?.materials = []
            mount.entity.removeFromParent()
        }
        mounts.removeAll()
        assetBySlotAtMount.removeAll()
        replacementBackups.removeAll()
        for mount in pendingRemovals.values {
            mount.entity.model?.materials = []
            mount.entity.removeFromParent()
        }
        pendingRemovals.removeAll()
    }

    // MARK: - Removal

    private var pendingRemovals: [String: PhotoMount] = [:]

    var pendingRemovalAssetIDs: Set<String> { Set(pendingRemovals.keys) }

    @discardableResult
    func beginRemoval(assetID: String) -> Bool {
        guard mounts[assetID] != nil else { return false }
        if liftedAssetID == assetID || targetAssetID == assetID {
            clearInteractionFeedback()
        }
        guard let mount = mounts.removeValue(forKey: assetID) else { return false }
        mount.entity.scale = .one
        mount.entity.removeFromParent()
        pendingRemovals[assetID] = mount
        return true
    }

    func revertRemoval(assetID: String) {
        guard let mount = pendingRemovals.removeValue(forKey: assetID) else { return }
        root.addChild(mount.entity)
        mounts[assetID] = mount
    }

    func commitRemoval(assetID: String) {
        guard let mount = pendingRemovals.removeValue(forKey: assetID) else { return }
        mount.entity.model?.materials = []
    }

    // MARK: - Replacement

    private struct ReplacementBackup {
        let materials: [any Material]
        let pixelWidth: Int
        let pixelHeight: Int
        let isTextured: Bool
        let hasFailed: Bool
    }

    private var replacementBackups: [String: ReplacementBackup] = [:]

    var previewingReplacementAssetIDs: Set<String> { Set(replacementBackups.keys) }

    func beginReplacementPreview(_ image: DecodedPhotoImage, forAsset assetID: String) async -> Bool {
        let generation = self.generation
        guard mounts[assetID] != nil else { return false }

        let texture: TextureResource
        do {
            texture = try await TextureResource(
                image: image.image,
                withName: "photo-replacement-\(assetID)",
                options: .init(semantic: .color, mipmapsMode: .allocateAndGenerateAll)
            )
        } catch {
            return false
        }
        guard generation == self.generation, var mount = mounts[assetID] else { return false }

        if replacementBackups[assetID] == nil {
            replacementBackups[assetID] = ReplacementBackup(
                materials: mount.entity.model?.materials ?? [],
                pixelWidth: mount.pixelWidth,
                pixelHeight: mount.pixelHeight,
                isTextured: mount.isTextured,
                hasFailed: mount.hasFailed
            )
        }
        mount.pixelWidth = image.pixelWidth
        mount.pixelHeight = image.pixelHeight
        mount.isTextured = true
        mount.hasFailed = false
        mount.entity.model?.materials = [Self.photoMaterial(texture)]
        refit(&mount)
        mounts[assetID] = mount
        return true
    }

    func revertReplacementPreview(forAsset assetID: String) {
        guard let backup = replacementBackups.removeValue(forKey: assetID), var mount = mounts[assetID] else { return }
        mount.entity.model?.materials = backup.materials
        mount.pixelWidth = backup.pixelWidth
        mount.pixelHeight = backup.pixelHeight
        mount.isTextured = backup.isTextured
        mount.hasFailed = backup.hasFailed
        refit(&mount)
        mounts[assetID] = mount
    }

    func commitReplacement(from previousAssetID: String, to replacementAssetID: String) {
        guard previousAssetID != replacementAssetID,
              var mount = mounts.removeValue(forKey: previousAssetID) else { return }
        replacementBackups.removeValue(forKey: previousAssetID)
        mount.entity.name = "photo-\(replacementAssetID)"
        mount.placement = ResolvedPhotoPlacement(
            slotIndex: mount.placement.slotIndex,
            photoAssetID: replacementAssetID,
            caption: mount.placement.caption,
            anchor: mount.placement.anchor,
            transform: mount.placement.transform
        )
        mounts[replacementAssetID] = mount
        if liftedAssetID == previousAssetID { liftedAssetID = replacementAssetID }
        if targetAssetID == previousAssetID { targetAssetID = replacementAssetID }
    }

    // MARK: - Interaction feedback (; focus added )

    private var liftedAssetID: String?
    private var targetAssetID: String?
    private var focusedAssetID: String?

    func setLifted(assetID: String?) {
        guard liftedAssetID != assetID else { return }
        let previous = liftedAssetID
        liftedAssetID = assetID
        if let previous { restoreAppearance(previous) }
        if let assetID { applyAppearance(assetID) }
    }

    func setTarget(assetID: String?) {
        guard targetAssetID != assetID else { return }
        let previous = targetAssetID
        targetAssetID = assetID
        if let previous { restoreAppearance(previous) }
        if let assetID { applyAppearance(assetID) }
    }

    func clearInteractionFeedback() {
        let affected = [liftedAssetID, targetAssetID].compactMap { $0 }
        liftedAssetID = nil
        targetAssetID = nil
        for assetID in affected { restoreAppearance(assetID) }
    }

    func setFocused(assetID: String?) {
        guard focusedAssetID != assetID else { return }
        let previous = focusedAssetID
        focusedAssetID = assetID
        if let previous { restoreAppearance(previous) }
        if let assetID { applyAppearance(assetID) }
    }

    var liftedAssetIDForTesting: String? { liftedAssetID }
    var targetAssetIDForTesting: String? { targetAssetID }
    var focusedPhotoAssetID: String? { focusedAssetID }
    var hasLiftedPhoto: Bool { liftedAssetID != nil }

    static let liftScale: Float = 1.08
    static let targetScale: Float = 1.04
    static let focusScale: Float = 1.02

    private func appearanceScale(_ assetID: String) -> Float {
        if assetID == liftedAssetID { return Self.liftScale }
        if assetID == targetAssetID { return Self.targetScale }
        if assetID == focusedAssetID { return Self.focusScale }
        return 1
    }

    private func applyAppearance(_ assetID: String) {
        setScale(appearanceScale(assetID), on: assetID)
    }

    private func restoreAppearance(_ assetID: String) {
        setScale(appearanceScale(assetID), on: assetID)
    }

    private func setScale(_ scale: Float, on assetID: String) {
        guard let mount = mounts[assetID] else { return }
        if UIAccessibility.isReduceMotionEnabled {
            mount.entity.scale = SIMD3<Float>(repeating: scale)
        } else {
            mount.entity.move(
                to: Transform(
                    scale: SIMD3<Float>(repeating: scale),
                    rotation: mount.placement.transform.rotation,
                    translation: mount.placement.transform.position
                ),
                relativeTo: mount.entity.parent,
                duration: 0.12
            )
        }
    }

    func mountedPlacements() -> [ResolvedPhotoPlacement] {
        mounts.values.map(\.placement)
    }

    // MARK: - Sizing

    private func refit(_ mount: inout PhotoMount) {
        let size = PhotoMountSizing.planeSize(
            envelope: mount.placement.transform,
            pixelWidth: mount.pixelWidth,
            pixelHeight: mount.pixelHeight
        )
        guard size.width > 0, size.height > 0 else { return }
        mount.entity.model?.mesh = .generatePlane(width: size.width, height: size.height)
    }

    // MARK: - Materials

    private static func photoMaterial(_ texture: TextureResource) -> PhysicallyBasedMaterial {
        var material = PhysicallyBasedMaterial()
        material.baseColor = .init(tint: .white, texture: .init(texture))
        material.roughness = .init(floatLiteral: 0.7)
        material.metallic = .init(floatLiteral: 0)
        return material
    }

    private static func placeholderMaterial() -> PhysicallyBasedMaterial {
        var material = PhysicallyBasedMaterial()
        material.baseColor = .init(tint: UIColor(white: 0.82, alpha: 1))
        material.roughness = .init(floatLiteral: 1)
        material.metallic = .init(floatLiteral: 0)
        return material
    }
}
