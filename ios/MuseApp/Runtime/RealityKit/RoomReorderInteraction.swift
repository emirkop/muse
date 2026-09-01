import RealityKit
import simd
import UIKit

@MainActor
final class RoomReorderInteraction: NSObject {
    static let targetRadius: CGFloat = RoomPhotoScreenHitTester.defaultRadius

    private weak var gestureHost: UIView?
    private weak var arView: ARView?
    private weak var layer: RoomPhotoLayer?
    private let isEditing: () -> Bool
    private let onSwap: (Int, Int) -> Void

    private(set) var recognizer: UILongPressGestureRecognizer?

    private(set) var liftedAssetID: String?
    private(set) var targetAssetID: String?

    init(
        gestureHost: UIView,
        arView: ARView,
        layer: RoomPhotoLayer,
        isEditing: @escaping () -> Bool,
        onSwap: @escaping (Int, Int) -> Void
    ) {
        self.gestureHost = gestureHost
        self.arView = arView
        self.layer = layer
        self.isEditing = isEditing
        self.onSwap = onSwap
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
            guard isEditing(), let pickedAssetID = nearestPhoto(to: point, in: layer) else {
                return
            }
            liftedAssetID = pickedAssetID
            targetAssetID = nil
            layer.setLifted(assetID: pickedAssetID)

        case .changed:
            guard let liftedAssetID else { return }
            let candidate = nearestPhoto(to: point, in: layer)
            let newTarget = (candidate == liftedAssetID) ? nil : candidate
            guard newTarget != targetAssetID else { return }
            targetAssetID = newTarget
            layer.setTarget(assetID: newTarget)

        case .ended:
            defer { clearFeedback() }
            guard let liftedAssetID, let targetAssetID,
                  let from = layer.slotIndex(forAsset: liftedAssetID),
                  let to = layer.slotIndex(forAsset: targetAssetID) else {
                return
            }
            onSwap(from, to)

        case .cancelled, .failed:
            clearFeedback()

        default:
            break
        }
    }

    private func clearFeedback() {
        liftedAssetID = nil
        targetAssetID = nil
        layer?.clearInteractionFeedback()
    }

    private func nearestPhoto(to point: CGPoint, in layer: RoomPhotoLayer) -> String? {
        guard let arView else { return nil }
        return RoomPhotoScreenHitTester.nearestPhotoAssetID(to: point, in: layer, projectedBy: arView)
    }

    // MARK: - Test seam

    func testBeginLift(assetID: String) {
        guard isEditing(), let layer, layer.slotIndex(forAsset: assetID) != nil else { return }
        liftedAssetID = assetID
        targetAssetID = nil
        layer.setLifted(assetID: assetID)
    }

    func testMove(over assetID: String?) {
        guard liftedAssetID != nil, let layer else { return }
        let newTarget = (assetID == liftedAssetID) ? nil : assetID
        targetAssetID = newTarget
        layer.setTarget(assetID: newTarget)
    }

    func testDrop() {
        defer { clearFeedback() }
        guard let liftedAssetID, let targetAssetID, let layer,
              let from = layer.slotIndex(forAsset: liftedAssetID),
              let to = layer.slotIndex(forAsset: targetAssetID) else {
            return
        }
        onSwap(from, to)
    }

    func testCancel() {
        clearFeedback()
    }
}
