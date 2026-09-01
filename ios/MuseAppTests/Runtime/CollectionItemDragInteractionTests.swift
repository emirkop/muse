import RealityKit
import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class CollectionItemDragInteractionTests: XCTestCase {

    private var arView: ARView!
    private var anchor: AnchorEntity!
    private let table = fixtureTierTable

    override func setUp() {
        arView = ARView(
            frame: CGRect(x: 0, y: 0, width: 320, height: 480),
            cameraMode: .nonAR,
            automaticallyConfigureSession: false
        )
        anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        arView = nil
        anchor = nil
    }

    private func items(_ slots: [Int]) -> [CollectionItem] {
        slots.map { CollectionItem(id: "item-\($0)", slotIndex: $0, catalogModelID: "model") }
    }

    private func placements(_ slots: [Int], tier: Int = 1)
        -> [(item: CollectionItem, slot: CollectionItemSlot)] {
        table.resolvePlacements(for: items(slots), atTier: CollectionTier(tier)).placed
    }

    private func emptySlots(occupied: [Int], tier: Int) -> [CollectionItemSlot] {
        table.availableSlots(atTier: CollectionTier(tier)).filter { !occupied.contains($0.slotIndex) }
    }

    // MARK: - The layer

    func test_applyMovesExistingEntitiesRatherThanRebuildingThem() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)

        layer.apply(placements([0, 1, 2]))
        let originals = Dictionary(
            uniqueKeysWithValues: ["item-0", "item-1", "item-2"].map { ($0, layer.position(forItem: $0)) }
        )
        XCTAssertEqual(layer.mountedItemIDs, ["item-0", "item-1", "item-2"])

        let swapped = [
            CollectionItem(id: "item-0", slotIndex: 2, catalogModelID: "model"),
            CollectionItem(id: "item-1", slotIndex: 1, catalogModelID: "model"),
            CollectionItem(id: "item-2", slotIndex: 0, catalogModelID: "model")
        ]
        layer.apply(table.resolvePlacements(for: swapped, atTier: .base).placed)

        XCTAssertEqual(layer.mountedItemIDs, ["item-0", "item-1", "item-2"], "no entity was rebuilt")
        XCTAssertEqual(layer.slotIndex(forItem: "item-0"), 2)
        XCTAssertEqual(layer.slotIndex(forItem: "item-2"), 0)
        XCTAssertEqual(layer.position(forItem: "item-0"), originals["item-2"] ?? nil)
        XCTAssertEqual(layer.position(forItem: "item-2"), originals["item-0"] ?? nil)
        XCTAssertEqual(layer.position(forItem: "item-1"), originals["item-1"] ?? nil)
    }

    func test_noItemCarriesACollisionComponent() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 1, 2, 3]))

        var checked = 0
        for entity in layer.root.children {
            XCTAssertNil(entity.components[CollisionComponent.self])
            checked += 1
        }
        XCTAssertEqual(checked, 4, "the scan found nothing — the test is broken, not the layer")
    }

    func test_anItemRemovedFromThePlacementsIsUnmounted() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 1, 2]))

        layer.apply(placements([0, 2]))

        XCTAssertEqual(layer.mountedItemIDs, ["item-0", "item-2"])
        XCTAssertNil(layer.position(forItem: "item-1"))
    }

    func test_itemIDAtSlotAnswersWhatIsActuallyMounted() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 3]))

        XCTAssertEqual(layer.itemID(atSlot: 0), "item-0")
        XCTAssertEqual(layer.itemID(atSlot: 3), "item-3")
        XCTAssertNil(layer.itemID(atSlot: 1), "an empty slot holds nothing")
    }

    // MARK: - The drag state machine

    private func makeInteraction(
        layer: CollectionItemLayer,
        editing: Bool = true,
        emptySlots: [CollectionItemSlot] = [],
        onDrop: @escaping (String, Int) -> Void = { _, _ in }
    ) -> CollectionItemDragInteraction {
        CollectionItemDragInteraction(
            gestureHost: UIView(),
            arView: arView,
            layer: layer,
            isEditing: { editing },
            availableEmptySlots: { emptySlots },
            onDrop: onDrop
        )
    }

    func test_liftingHighlightsTheItemAndADropOnAnotherReportsItsSlot() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 1, 2]))

        var drops: [(String, Int)] = []
        let interaction = makeInteraction(layer: layer) { drops.append(($0, $1)) }

        interaction.testBeginLift(itemID: "item-0")
        XCTAssertEqual(layer.liftedItemID, "item-0")

        interaction.testMove(overItem: "item-2")
        XCTAssertEqual(layer.targetItemID, "item-2")
        XCTAssertEqual(interaction.targetSlotIndex, 2)

        interaction.testDrop()
        XCTAssertEqual(drops.count, 1)
        XCTAssertEqual(drops.first?.0, "item-0")
        XCTAssertEqual(drops.first?.1, 2)
        XCTAssertNil(layer.liftedItemID)
        XCTAssertNil(layer.targetItemID)
    }

    func test_anEmptyAvailableSlotIsAValidDropTarget() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 1, 2]))

        var drops: [(String, Int)] = []
        let interaction = makeInteraction(
            layer: layer,
            emptySlots: emptySlots(occupied: [0, 1, 2], tier: 1),
            onDrop: { drops.append(($0, $1)) }
        )

        interaction.testBeginLift(itemID: "item-1")
        interaction.testMove(overEmptySlot: 3)
        XCTAssertEqual(interaction.targetSlotIndex, 3)
        XCTAssertNil(layer.targetItemID, "an empty slot is not an item target")
        XCTAssertEqual(layer.targetEmptySlotIndex, 3)

        interaction.testDrop()
        XCTAssertEqual(drops.map(\.1), [3])
    }

    func test_aFutureTierSlotIsNeverATarget() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 1]))

        var drops: [(String, Int)] = []
        let interaction = makeInteraction(
            layer: layer,
            emptySlots: emptySlots(occupied: [0, 1], tier: 1),
            onDrop: { drops.append(($0, $1)) }
        )

        interaction.testBeginLift(itemID: "item-0")
        interaction.testMove(overEmptySlot: 7)
        XCTAssertNil(interaction.targetSlotIndex)

        interaction.testDrop()
        XCTAssertTrue(drops.isEmpty, "a future-tier slot must not produce a drop")
    }

    func test_hoveringBackOverTheSourceClearsTheTargetAndCancelsTheDrop() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 1]))

        var drops: [(String, Int)] = []
        let interaction = makeInteraction(layer: layer) { drops.append(($0, $1)) }

        interaction.testBeginLift(itemID: "item-0")
        interaction.testMove(overItem: "item-1")
        XCTAssertEqual(interaction.targetSlotIndex, 1)

        interaction.testMove(overItem: "item-0")
        XCTAssertNil(interaction.targetSlotIndex)

        interaction.testDrop()
        XCTAssertTrue(drops.isEmpty, "a drop on nothing is a cancellation")
    }

    func test_nothingCanBeLiftedWhenRearrangingIsOff() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 1]))

        let interaction = makeInteraction(layer: layer, editing: false)
        interaction.testBeginLift(itemID: "item-0")

        XCTAssertNil(interaction.liftedItemID)
        XCTAssertNil(layer.liftedItemID)
    }

    func test_detachClearsFeedback() {
        let layer = CollectionItemLayer()
        anchor.addChild(layer.root)
        layer.apply(placements([0, 1]))

        let interaction = makeInteraction(layer: layer)
        interaction.testBeginLift(itemID: "item-0")
        interaction.detach()

        XCTAssertNil(layer.liftedItemID)
        XCTAssertNil(interaction.recognizer)
    }
}
