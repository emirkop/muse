import RealityKit
import UIKit

@MainActor
final class RoomPhotoTapInteraction: NSObject {
    private weak var gestureHost: UIView?
    private weak var arView: ARView?
    private weak var layer: RoomPhotoLayer?
    private let isEditing: () -> Bool
    private let onPhotoTapped: (String) -> Void

    private(set) var recognizer: UITapGestureRecognizer?

    init(
        gestureHost: UIView,
        arView: ARView,
        layer: RoomPhotoLayer,
        requiringFailureOf longPress: UILongPressGestureRecognizer?,
        isEditing: @escaping () -> Bool,
        onPhotoTapped: @escaping (String) -> Void
    ) {
        self.gestureHost = gestureHost
        self.arView = arView
        self.layer = layer
        self.isEditing = isEditing
        self.onPhotoTapped = onPhotoTapped
        super.init()

        let recognizer = UITapGestureRecognizer(target: self, action: #selector(handleTap))
        recognizer.numberOfTapsRequired = 1
        recognizer.cancelsTouchesInView = false
        if let longPress {
            recognizer.require(toFail: longPress)
        }
        gestureHost.addGestureRecognizer(recognizer)
        self.recognizer = recognizer
    }

    func detach() {
        if let recognizer, let gestureHost {
            gestureHost.removeGestureRecognizer(recognizer)
        }
        recognizer = nil
    }

    @objc private func handleTap(_ gesture: UITapGestureRecognizer) {
        guard gesture.state == .ended, let arView, let layer, isEditing() else { return }
        let point = gesture.location(in: arView)
        guard let assetID = RoomPhotoScreenHitTester.nearestPhotoAssetID(to: point, in: layer, projectedBy: arView) else {
            return
        }
        onPhotoTapped(assetID)
    }

    // MARK: - Test seam
    func testTap(assetID: String) {
        guard isEditing(), let layer, layer.slotIndex(forAsset: assetID) != nil else { return }
        onPhotoTapped(assetID)
    }
}
