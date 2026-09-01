import RealityKit
import simd
import UIKit

@MainActor
final class CollectionItemDragInteraction: NSObject {
    private weak var gestureHost: UIView?
    private weak var arView: ARView?
    private weak var layer: CollectionItemLayer?

    private let isEditing: () -> Bool
    private let availableEmptySlots: () -> [CollectionItemSlot]
    private let onDrop: (_ itemID: String, _ slotIndex: Int) -> Void

    private(set) var recognizer: UILongPressGestureRecognizer?

    private(set) var liftedItemID: String?
    private(set) var targetItemID: String?
    private(set) var targetSlotIndex: Int?

    init(
        gestureHost: UIView,
        arView: ARView,
        layer: CollectionItemLayer,
        isEditing: @escaping () -> Bool,
        availableEmptySlots: @escaping () -> [CollectionItemSlot],
        onDrop: @escaping (_ itemID: String, _ slotIndex: Int) -> Void
    ) {
        self.gestureHost = gestureHost
        self.arView = arView
        self.layer = layer
        self.isEditing = isEditing
        self.availableEmptySlots = availableEmptySlots
        self.onDrop = onDrop
        super.init()

        let recognizer = UILongPressGestureRecognizer(target: self, action: #selector(handleLongPress))
        recognizer.minimumPressDuration = 0.35
        recognizer.allowableMovement = .greatestFiniteMagnitude
        recognizer.cancelsTouchesInView = false
        gestureHost.addGestureRecognizer(recognizer)
        self.recognizer = recognizer
    }

    func detach() {
        clearFeedback()
        if let recognizer, let gestureHost {
            gestureHost.removeGestureRecognizer(recognizer)
        }
        recognizer = nil
    }

    // MARK: - Gesture

    @objc private func handleLongPress(_ gesture: UILongPressGestureRecognizer) {
        guard let arView, let layer else { return }
        let point = gesture.location(in: arView)

        switch gesture.state {
        case .began:
            guard isEditing() else { return }
            guard case .item(let pickedItemID)? = CollectionItemScreenHitTester.nearestTarget(
                to: point, in: layer, emptySlots: [], projectedBy: arView
            ) else {
                return
            }
            beginLift(itemID: pickedItemID, layer: layer)

        case .changed:
            guard let liftedItemID else { return }
            let hit = CollectionItemScreenHitTester.nearestTarget(
                to: point,
                in: layer,
                emptySlots: availableEmptySlots(),
                projectedBy: arView,
                excludingItemID: liftedItemID
            )
            apply(hit: hit, layer: layer)

        case .ended:
            defer { clearFeedback() }
            guard let liftedItemID, let targetSlotIndex else {
                return
            }
            onDrop(liftedItemID, targetSlotIndex)

        case .cancelled, .failed:
            clearFeedback()

        default:
            break
        }
    }

    private func beginLift(itemID: String, layer: CollectionItemLayer) {
        liftedItemID = itemID
        targetItemID = nil
        targetSlotIndex = nil
        layer.setLifted(itemID: itemID)
        layer.setTarget(itemID: nil, emptySlotIndex: nil)
    }

    private func apply(hit: CollectionItemScreenHitTester.Hit?, layer: CollectionItemLayer) {
        switch hit {
        case .item(let itemID):
            guard let slot = layer.slotIndex(forItem: itemID) else { return }
            guard targetItemID != itemID else { return }
            targetItemID = itemID
            targetSlotIndex = slot
            layer.setTarget(itemID: itemID, emptySlotIndex: nil)
        case .emptySlot(let slotIndex):
            guard targetSlotIndex != slotIndex || targetItemID != nil else { return }
            targetItemID = nil
            targetSlotIndex = slotIndex
            layer.setTarget(itemID: nil, emptySlotIndex: slotIndex)
        case nil:
            guard targetItemID != nil || targetSlotIndex != nil else { return }
            targetItemID = nil
            targetSlotIndex = nil
            layer.setTarget(itemID: nil, emptySlotIndex: nil)
        }
    }

    private func clearFeedback() {
        liftedItemID = nil
        targetItemID = nil
        targetSlotIndex = nil
        layer?.clearInteractionFeedback()
    }

    // MARK: - Test seam

    func testBeginLift(itemID: String) {
        guard isEditing(), let layer, layer.slotIndex(forItem: itemID) != nil else { return }
        beginLift(itemID: itemID, layer: layer)
    }

    func testMove(overItem itemID: String?) {
        guard liftedItemID != nil, let layer else { return }
        guard let itemID, itemID != liftedItemID else {
            apply(hit: nil, layer: layer)
            return
        }
        apply(hit: .item(itemID: itemID), layer: layer)
    }

    func testMove(overEmptySlot slotIndex: Int) {
        guard liftedItemID != nil, let layer else { return }
        guard availableEmptySlots().contains(where: { $0.slotIndex == slotIndex }) else {
            apply(hit: nil, layer: layer)
            return
        }
        apply(hit: .emptySlot(slotIndex: slotIndex), layer: layer)
    }

    func testDrop() {
        defer { clearFeedback() }
        guard let liftedItemID, let targetSlotIndex else { return }
        onDrop(liftedItemID, targetSlotIndex)
    }

    func testCancel() {
        clearFeedback()
    }
}
