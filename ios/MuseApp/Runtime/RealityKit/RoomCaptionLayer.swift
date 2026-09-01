import RealityKit
import simd
import UIKit

@MainActor
final class RoomCaptionLayer {
    let root = Entity()

    static let gap: Float = 0.05
    static let heightFraction: Float = 0.16
    static let minHeight: Float = 0.055
    static let maxHeight: Float = 0.11
    static let pixelsPerMetre: CGFloat = 1_100

    private struct Plaque {
        let entity: ModelEntity
        var caption: String
        var placement: ResolvedPhotoPlacement
    }

    private var plaques: [String: Plaque] = [:]

    init() {
        root.name = "RoomCaptionLayer"
    }

    // MARK: - Read-outs

    var captionedAssetIDs: Set<String> { Set(plaques.keys) }

    func caption(forAsset assetID: String) -> String? { plaques[assetID]?.caption }

    func plaquePosition(forAsset assetID: String) -> SIMD3<Float>? {
        plaques[assetID]?.entity.position
    }

    func plaqueBounds(forAsset assetID: String) -> SIMD3<Float>? {
        plaques[assetID]?.entity.model?.mesh.bounds.extents
    }

    // MARK: - Reconciliation

    func apply(_ placements: [ResolvedPhotoPlacement]) {
        var seen: Set<String> = []

        for placement in placements {
            let caption = CaptionRules.normalised(placement.caption)
            guard !caption.isEmpty else { continue }
            seen.insert(placement.photoAssetID)

            if var existing = plaques[placement.photoAssetID] {
                let captionChanged = existing.caption != caption
                let moved = existing.placement.transform != placement.transform
                existing.caption = caption
                existing.placement = placement
                if moved {
                    position(existing.entity, for: placement)
                }
                if captionChanged || moved {
                    apply(caption: caption, to: existing.entity, for: placement)
                }
                plaques[placement.photoAssetID] = existing
            } else {
                let entity = ModelEntity()
                entity.name = "caption-\(placement.photoAssetID)"
                position(entity, for: placement)
                apply(caption: caption, to: entity, for: placement)
                root.addChild(entity)
                plaques[placement.photoAssetID] = Plaque(entity: entity, caption: caption, placement: placement)
            }
        }

        for (assetID, plaque) in plaques where !seen.contains(assetID) {
            plaque.entity.model?.materials = []
            plaque.entity.removeFromParent()
            plaques.removeValue(forKey: assetID)
        }
    }

    func tearDown() {
        for plaque in plaques.values {
            plaque.entity.model?.materials = []
            plaque.entity.removeFromParent()
        }
        plaques.removeAll()
    }

    // MARK: - Geometry

    private func position(_ entity: ModelEntity, for placement: ResolvedPhotoPlacement) {
        let envelope = placement.transform.scale
        let height = plaqueHeight(for: envelope)
        let drop = envelope.y / 2 + Self.gap + height / 2
        let localOffset = SIMD3<Float>(0, -drop, 0)
        let rotated = simd_act(placement.transform.rotation, localOffset)

        entity.transform = Transform(
            scale: .one,
            rotation: placement.transform.rotation,
            translation: placement.transform.position + rotated
        )
    }

    private func plaqueHeight(for envelope: SIMD3<Float>) -> Float {
        min(max(envelope.y * Self.heightFraction, Self.minHeight), Self.maxHeight)
    }

    private func apply(caption: String, to entity: ModelEntity, for placement: ResolvedPhotoPlacement) {
        let envelope = placement.transform.scale
        let width = envelope.x
        let height = plaqueHeight(for: envelope)

        guard let image = Self.renderCaptionImage(caption, widthMetres: width, heightMetres: height) else {
            entity.model = nil
            return
        }

        var material = PhysicallyBasedMaterial()
        material.roughness = .init(floatLiteral: 0.9)
        material.metallic = .init(floatLiteral: 0)
        do {
            let texture = try TextureResource(
                image: image,
                withName: "caption-\(placement.photoAssetID)",
                options: .init(semantic: .color, mipmapsMode: .allocateAndGenerateAll)
            )
            material.baseColor = .init(tint: .white, texture: .init(texture))
        } catch {
            entity.model = nil
            return
        }

        entity.model = ModelComponent(
            mesh: .generatePlane(width: width, height: height, cornerRadius: height * 0.12),
            materials: [material]
        )
        entity.collision = nil
        entity.components.set(OpacityComponent(opacity: 1))
    }

    // MARK: - Text

    static func renderCaptionImage(_ caption: String, widthMetres: Float, heightMetres: Float) -> CGImage? {
        let pixelWidth = max(64, Int(CGFloat(widthMetres) * pixelsPerMetre))
        let pixelHeight = max(24, Int(CGFloat(heightMetres) * pixelsPerMetre))
        let size = CGSize(width: pixelWidth, height: pixelHeight)

        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.opaque = true

        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            UIColor(white: 0.09, alpha: 1).setFill()
            context.fill(CGRect(origin: .zero, size: size))

            UIColor(white: 0.32, alpha: 1).setStroke()
            let border = UIBezierPath(rect: CGRect(origin: .zero, size: size).insetBy(dx: 1, dy: 1))
            border.lineWidth = 2
            border.stroke()

            let inset = CGFloat(pixelHeight) * 0.16
            let textRect = CGRect(origin: .zero, size: size).insetBy(dx: inset, dy: inset)

            let baseSize = CGFloat(pixelHeight) * 0.42
            let scaled = UIFontMetrics(forTextStyle: .caption1).scaledValue(for: baseSize)
            let fontSize = min(scaled, CGFloat(pixelHeight) * 0.6)

            let paragraph = NSMutableParagraphStyle()
            paragraph.alignment = .center
            paragraph.lineBreakMode = .byTruncatingTail

            (caption as NSString).draw(
                with: textRect,
                options: [.usesLineFragmentOrigin, .truncatesLastVisibleLine],
                attributes: [
                    .font: UIFont.systemFont(ofSize: fontSize, weight: .medium),
                    .foregroundColor: UIColor(white: 0.96, alpha: 1),
                    .paragraphStyle: paragraph
                ],
                context: nil
            )
        }
        return image.cgImage
    }
}
