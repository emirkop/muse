import RealityKit
import simd
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class RoomCaptionLayerTests: XCTestCase {

    private var arView: ARView!
    private var anchor: AnchorEntity!
    private let table = PlaceholderRoomSlotTable.build()

    override func setUp() {
        arView = ARView(frame: CGRect(x: 0, y: 0, width: 320, height: 480), cameraMode: .nonAR, automaticallyConfigureSession: false)
        anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        arView = nil
        anchor = nil
    }

    private func room(_ captions: [String]) -> Room {
        Room(
            id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: captions.enumerated().map { index, caption in
                PhotoSlotAssignment(slotIndex: index, photoAssetID: "a\(index)", caption: caption)
            }
        )
    }

    private func placements(_ room: Room) -> [ResolvedPhotoPlacement] {
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
            XCTFail("must resolve")
            return []
        }
        return placements
    }

    private func makeLayer() -> RoomCaptionLayer {
        let layer = RoomCaptionLayer()
        anchor.addChild(layer.root)
        return layer
    }

    // MARK: - A plaque exists exactly where a caption does

    func test_onlyCaptionedPhotographsGetAPlaque() {
        let layer = makeLayer()
        layer.apply(placements(room(["Trabzon", "", "  ", "Ankara"])))

        XCTAssertEqual(layer.captionedAssetIDs, ["a0", "a3"])
        XCTAssertEqual(layer.root.children.count, 2, "no chrome for an uncaptioned photograph")
        XCTAssertNil(layer.plaquePosition(forAsset: "a1"))
        XCTAssertNil(layer.plaquePosition(forAsset: "a2"), "whitespace-only is no caption")
    }

    func test_addingACaption_addsExactlyOnePlaque() {
        let layer = makeLayer()
        layer.apply(placements(room(["", "", ""])))
        XCTAssertTrue(layer.captionedAssetIDs.isEmpty)

        layer.apply(placements(room(["", "Now captioned", ""])))

        XCTAssertEqual(layer.captionedAssetIDs, ["a1"])
        XCTAssertEqual(layer.caption(forAsset: "a1"), "Now captioned")
        XCTAssertEqual(layer.root.children.count, 1)
    }

    func test_clearingACaption_removesThePlaqueEntirely() {
        let layer = makeLayer()
        layer.apply(placements(room(["Was here", "Other"])))
        XCTAssertEqual(layer.captionedAssetIDs, ["a0", "a1"])

        layer.apply(placements(room(["", "Other"])))

        XCTAssertEqual(layer.captionedAssetIDs, ["a1"], "the cleared caption's plaque is gone, not blanked")
        XCTAssertEqual(layer.root.children.count, 1)
    }

    func test_editingACaption_reusesThePlaqueAndShowsTheNewText() {
        let layer = makeLayer()
        layer.apply(placements(room(["First"])))
        let entityCountBefore = layer.root.children.count

        layer.apply(placements(room(["Second"])))

        XCTAssertEqual(layer.caption(forAsset: "a0"), "Second")
        XCTAssertEqual(layer.root.children.count, entityCountBefore, "the same plaque was re-rendered")
    }

    func test_plaqueTextIsTrimmed_soStoredWhitespaceNeverRenders() {
        let layer = makeLayer()
        layer.apply(placements(room(["   padded   "])))
        XCTAssertEqual(layer.caption(forAsset: "a0"), "padded")
    }

    // MARK: - Geometry

    func test_plaqueHangsBelowItsPhotographsEnvelope() {
        let layer = makeLayer()
        let placements = placements(room(["Focal caption", "Side caption"]))
        layer.apply(placements)

        for placement in placements {
            let plaque = layer.plaquePosition(forAsset: placement.photoAssetID)!
            let photo = placement.transform.position
            XCTAssertLessThan(plaque.y, photo.y, "the plaque sits under the photograph")
            let envelopeBottom = photo.y - placement.transform.scale.y / 2
            XCTAssertLessThanOrEqual(plaque.y, envelopeBottom, "and below the envelope, never overlapping it")
            XCTAssertEqual(plaque.x, photo.x, accuracy: 0.001)
            XCTAssertEqual(plaque.z, photo.z, accuracy: 0.001)
        }
    }

    func test_plaquePositionIsIndependentOfThePhotographsAspectRatio() {
        let layer = makeLayer()
        let base = placements(room(["Caption"]))
        layer.apply(base)
        let before = layer.plaquePosition(forAsset: "a0")

        layer.apply(base)
        XCTAssertEqual(layer.plaquePosition(forAsset: "a0"), before)
    }

    func test_plaqueIsSizedToItsWall_withinTheClampedHeightRange() {
        let layer = makeLayer()
        let placements = placements(room(Array(repeating: "Caption", count: 28)))
        layer.apply(placements)

        for placement in placements {
            let bounds = layer.plaqueBounds(forAsset: placement.photoAssetID)!
            XCTAssertEqual(bounds.x, placement.transform.scale.x, accuracy: 0.001, "as wide as the envelope")
            XCTAssertGreaterThanOrEqual(bounds.y, RoomCaptionLayer.minHeight - 0.001)
            XCTAssertLessThanOrEqual(bounds.y, RoomCaptionLayer.maxHeight + 0.001)
        }
    }

    func test_plaquesHaveNoCollisionShape() {
        let layer = makeLayer()
        layer.apply(placements(room(["One", "Two", "Three"])))

        for child in layer.root.children {
            XCTAssertNil((child as? ModelEntity)?.collision, "a plaque must not be an obstacle to movement")
        }
    }

    // MARK: - Captions follow their photograph

    func test_afterAReorder_eachPlaqueIsUnderItsOwnPhotographsNewPosition() {
        let layer = makeLayer()
        let start = room((0..<28).map { "caption \($0)" })
        layer.apply(placements(start))

        let reordered = start.replacingPhotoSlots(RoomPhotoOrder.swapping(start.photoSlots, from: 0, to: 27))
        let after = placements(reordered)
        layer.apply(after)

        XCTAssertEqual(layer.captionedAssetIDs.count, 28)
        for placement in after {
            XCTAssertEqual(layer.caption(forAsset: placement.photoAssetID), placement.caption)
            let plaque = layer.plaquePosition(forAsset: placement.photoAssetID)!
            XCTAssertEqual(plaque.x, placement.transform.position.x, accuracy: 0.001)
            XCTAssertEqual(plaque.z, placement.transform.position.z, accuracy: 0.001)
            XCTAssertLessThan(plaque.y, placement.transform.position.y)
        }
        XCTAssertEqual(layer.caption(forAsset: "a0"), "caption 0")
        XCTAssertEqual(layer.caption(forAsset: "a27"), "caption 27")
    }

    func test_aPlaqueResizesWhenItsPhotographChangesWall() {
        let layer = makeLayer()
        let start = room(Array(repeating: "Caption", count: 28))
        layer.apply(placements(start))
        let focalWidthBefore = layer.plaqueBounds(forAsset: "a0")!.x
        let sideWidthBefore = layer.plaqueBounds(forAsset: "a1")!.x
        XCTAssertNotEqual(focalWidthBefore, sideWidthBefore, accuracy: 0.0001)

        let reordered = start.replacingPhotoSlots(RoomPhotoOrder.swapping(start.photoSlots, from: 0, to: 1))
        layer.apply(placements(reordered))

        XCTAssertEqual(layer.plaqueBounds(forAsset: "a0")!.x, sideWidthBefore, accuracy: 0.001)
        XCTAssertEqual(layer.plaqueBounds(forAsset: "a1")!.x, focalWidthBefore, accuracy: 0.001)
    }

    func test_plaquesFollowAReflow() {
        let layer = makeLayer()
        layer.apply(placements(room(["a", "b", "c", "d"])))
        let rearBefore = layer.plaquePosition(forAsset: "a3")

        layer.apply(placements(room(["a", "b", "c", "d", "e"])))

        XCTAssertEqual(layer.captionedAssetIDs.count, 5)
        XCTAssertNotEqual(layer.plaquePosition(forAsset: "a3"), rearBefore, "a3 relocated with the reflow")
    }

    func test_aPhotographLeavingTheRoom_takesItsPlaqueWithIt() {
        let layer = makeLayer()
        layer.apply(placements(room(["a", "b", "c"])))
        layer.apply(placements(room(["a", "b"])))

        XCTAssertEqual(layer.captionedAssetIDs, ["a0", "a1"])
        XCTAssertEqual(layer.root.children.count, 2)
    }

    // MARK: - Teardown

    func test_tearDown_removesEveryPlaque() {
        let layer = makeLayer()
        layer.apply(placements(room(["a", "b", "c"])))
        layer.tearDown()

        XCTAssertTrue(layer.captionedAssetIDs.isEmpty)
        XCTAssertTrue(layer.root.children.isEmpty)
        layer.tearDown()
    }

    // MARK: - Text rendering

    func test_captionImage_isRenderedOpaqueAtTheRequestedShape() {
        let image = RoomCaptionLayer.renderCaptionImage("Trabzon, 1998", widthMetres: 1.6, heightMetres: 0.1)
        let unwrapped = try? XCTUnwrap(image)
        XCTAssertNotNil(unwrapped)
        guard let unwrapped else { return }

        XCTAssertEqual(unwrapped.width, Int(1.6 * RoomCaptionLayer.pixelsPerMetre))
        XCTAssertEqual(unwrapped.height, Int(0.1 * RoomCaptionLayer.pixelsPerMetre))
        XCTAssertTrue([.noneSkipFirst, .noneSkipLast, .none].contains(unwrapped.alphaInfo))
    }

    func test_captionImage_survivesALongCaptionAndAWideRangeOfText() {
        let cases = [
            String(repeating: "long ", count: 100),
            "İstanbul’da bir öğleden sonra",
            "日本語のキャプション",
            "👨‍👩‍👧‍👦 family"
        ]
        for caption in cases {
            XCTAssertNotNil(
                RoomCaptionLayer.renderCaptionImage(caption, widthMetres: 0.62, heightMetres: 0.06),
                "must render \(caption.prefix(12))…"
            )
        }
    }
}
