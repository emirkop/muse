import RealityKit
import simd
import UIKit

@MainActor
final class LobbyCardLayer {
    let root = Entity()

    static let pixelsPerMetre: CGFloat = 600
    static let focusFrameScale: Float = 1.07

    private struct Card {
        let entity: ModelEntity
        let focusFrame: ModelEntity
        var placement: ResolvedLobbyCardPlacement
    }

    private var cards: [String: Card] = [:]
    private(set) var focusedRoomID: String?

    init() {
        root.name = "LobbyCardLayer"
    }

    // MARK: - Read-outs

    var mountedRoomIDs: Set<String> { Set(cards.keys) }

    func position(forRoom roomID: String) -> SIMD3<Float>? { cards[roomID]?.entity.position }

    func cardIndex(forRoom roomID: String) -> Int? { cards[roomID]?.placement.cardIndex }

    func isFocused(roomID: String) -> Bool { focusedRoomID == roomID }

    func isFocusFrameVisible(roomID: String) -> Bool { cards[roomID]?.focusFrame.isEnabled ?? false }

    func mountedCardPositions() -> [(roomID: String, worldPosition: SIMD3<Float>)] {
        cards.map { ($0.key, $0.value.entity.position) }
    }

    // MARK: - Reconciliation

    func apply(_ placements: [ResolvedLobbyCardPlacement]) {
        var seen: Set<String> = []

        for placement in placements {
            seen.insert(placement.roomID)

            if var existing = cards[placement.roomID] {
                let moved = existing.placement.transform != placement.transform
                let relabelled = existing.placement.card != placement.card
                existing.placement = placement
                if moved {
                    position(existing.entity, existing.focusFrame, for: placement)
                }
                if relabelled || moved {
                    applySignage(placement.card, to: existing.entity, envelope: placement.transform.scale)
                }
                cards[placement.roomID] = existing
            } else {
                let entity = ModelEntity()
                entity.name = "lobby-card-\(placement.roomID)"
                let frame = Self.makeFocusFrame(envelope: placement.transform.scale)
                entity.addChild(frame)
                position(entity, frame, for: placement)
                applySignage(placement.card, to: entity, envelope: placement.transform.scale)
                root.addChild(entity)
                cards[placement.roomID] = Card(entity: entity, focusFrame: frame, placement: placement)
            }
        }

        for (roomID, card) in cards where !seen.contains(roomID) {
            remove(card, roomID: roomID)
        }

        if let focusedRoomID, cards[focusedRoomID] == nil {
            setFocused(roomID: nil)
        }
    }

    func setFocused(roomID: String?) {
        guard roomID != focusedRoomID else { return }
        if let previous = focusedRoomID, let card = cards[previous] {
            card.focusFrame.isEnabled = false
        }
        focusedRoomID = roomID
        if let roomID, let card = cards[roomID] {
            card.focusFrame.isEnabled = true
        }
    }

    func tearDown() {
        for (roomID, card) in cards {
            remove(card, roomID: roomID)
        }
        cards.removeAll()
        focusedRoomID = nil
    }

    private func remove(_ card: Card, roomID: String) {
        card.focusFrame.model?.materials = []
        card.focusFrame.removeFromParent()
        card.entity.model?.materials = []
        card.entity.removeFromParent()
        cards.removeValue(forKey: roomID)
    }

    // MARK: - Geometry

    private func position(_ entity: ModelEntity, _ frame: ModelEntity, for placement: ResolvedLobbyCardPlacement) {
        entity.transform = Transform(
            scale: .one,
            rotation: placement.transform.rotation,
            translation: placement.transform.position
        )
        frame.position = SIMD3<Float>(0, 0, -0.012)
    }

    private static func makeFocusFrame(envelope: SIMD3<Float>) -> ModelEntity {
        var material = PhysicallyBasedMaterial()
        material.baseColor = .init(tint: UIColor(red: 0.99, green: 0.85, blue: 0.42, alpha: 1))
        material.roughness = .init(floatLiteral: 0.4)
        material.metallic = .init(floatLiteral: 0)

        let frame = ModelEntity(
            mesh: .generatePlane(
                width: envelope.x * focusFrameScale,
                height: envelope.y * focusFrameScale,
                cornerRadius: envelope.y * 0.06
            ),
            materials: [material]
        )
        frame.name = "lobby-card-focus-frame"
        frame.isEnabled = false
        frame.collision = nil
        return frame
    }

    private func applySignage(_ card: LobbyRoomCard, to entity: ModelEntity, envelope: SIMD3<Float>) {
        guard let image = Self.renderCardImage(
            card,
            widthMetres: envelope.x,
            heightMetres: envelope.y
        ) else {
            entity.model = nil
            return
        }

        var material = PhysicallyBasedMaterial()
        material.roughness = .init(floatLiteral: 0.85)
        material.metallic = .init(floatLiteral: 0)
        do {
            let texture = try TextureResource(
                image: image,
                withName: "lobby-card-\(card.roomID)",
                options: .init(semantic: .color, mipmapsMode: .allocateAndGenerateAll)
            )
            material.baseColor = .init(tint: .white, texture: .init(texture))
        } catch {
            entity.model = nil
            return
        }

        entity.model = ModelComponent(
            mesh: .generatePlane(width: envelope.x, height: envelope.y, cornerRadius: envelope.y * 0.05),
            materials: [material]
        )
        entity.collision = nil
    }

    // MARK: - Signage

    static func renderCardImage(
        _ card: LobbyRoomCard,
        widthMetres: Float,
        heightMetres: Float
    ) -> CGImage? {
        let pixelWidth = max(128, Int(CGFloat(widthMetres) * pixelsPerMetre))
        let pixelHeight = max(96, Int(CGFloat(heightMetres) * pixelsPerMetre))
        let size = CGSize(width: pixelWidth, height: pixelHeight)

        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.opaque = true

        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            UIColor(white: 0.11, alpha: 1).setFill()
            context.fill(CGRect(origin: .zero, size: size))

            UIColor(white: 0.38, alpha: 1).setStroke()
            let border = UIBezierPath(rect: CGRect(origin: .zero, size: size).insetBy(dx: 2, dy: 2))
            border.lineWidth = 4
            border.stroke()

            let inset = CGFloat(pixelHeight) * 0.12
            var textRect = CGRect(origin: .zero, size: size).insetBy(dx: inset, dy: inset)

            if card.isMarkedPrivate {
                let markingHeight = CGFloat(pixelHeight) * 0.18
                let markingRect = CGRect(
                    x: textRect.minX,
                    y: textRect.maxY - markingHeight,
                    width: textRect.width,
                    height: markingHeight
                )
                drawPrivateMarking(in: markingRect, pixelHeight: pixelHeight)
                textRect = textRect.inset(by: UIEdgeInsets(top: 0, left: 0, bottom: markingHeight, right: 0))
            }

            let baseSize = CGFloat(pixelHeight) * 0.2
            let scaled = UIFontMetrics(forTextStyle: .title2).scaledValue(for: baseSize)
            let fontSize = min(scaled, CGFloat(pixelHeight) * 0.3)

            let paragraph = NSMutableParagraphStyle()
            paragraph.alignment = .center
            paragraph.lineBreakMode = .byTruncatingTail

            let attributes: [NSAttributedString.Key: Any] = [
                .font: UIFont.systemFont(ofSize: fontSize, weight: .semibold),
                .foregroundColor: UIColor(white: 0.97, alpha: 1),
                .paragraphStyle: paragraph
            ]
            let name = card.signageText as NSString
            let bounding = name.boundingRect(
                with: CGSize(width: textRect.width, height: textRect.height),
                options: [.usesLineFragmentOrigin],
                attributes: attributes,
                context: nil
            )
            let drawRect = CGRect(
                x: textRect.minX,
                y: textRect.minY + max(0, (textRect.height - bounding.height) / 2),
                width: textRect.width,
                height: min(bounding.height, textRect.height)
            )
            name.draw(
                with: drawRect,
                options: [.usesLineFragmentOrigin, .truncatesLastVisibleLine],
                attributes: attributes,
                context: nil
            )
        }
        return image.cgImage
    }

    private static func drawPrivateMarking(in rect: CGRect, pixelHeight: Int) {
        let paragraph = NSMutableParagraphStyle()
        paragraph.alignment = .center
        let fontSize = min(rect.height * 0.62, CGFloat(pixelHeight) * 0.12)
        ("PRIVATE" as NSString).draw(
            with: rect,
            options: [.usesLineFragmentOrigin],
            attributes: [
                .font: UIFont.systemFont(ofSize: fontSize, weight: .heavy),
                .foregroundColor: UIColor(red: 1, green: 0.72, blue: 0.35, alpha: 1),
                .kern: fontSize * 0.16,
                .paragraphStyle: paragraph
            ],
            context: nil
        )
    }
}
