import RealityKit
import simd
import UIKit

@MainActor
final class LobbyCardInteraction: NSObject {
    private weak var gestureHost: UIView?
    private weak var arView: ARView?
    private weak var layer: LobbyCardLayer?
    private var placements: [ResolvedLobbyCardPlacement]
    private let onEnterRoom: (String) -> Void
    private let onDistantCardTapped: (LobbyRoomCard) -> Void

    private(set) var recognizer: UITapGestureRecognizer?

    init(
        gestureHost: UIView,
        arView: ARView,
        layer: LobbyCardLayer,
        placements: [ResolvedLobbyCardPlacement],
        onEnterRoom: @escaping (String) -> Void,
        onDistantCardTapped: @escaping (LobbyRoomCard) -> Void = { _ in }
    ) {
        self.gestureHost = gestureHost
        self.arView = arView
        self.layer = layer
        self.placements = placements
        self.onEnterRoom = onEnterRoom
        self.onDistantCardTapped = onDistantCardTapped
        super.init()

        let recognizer = UITapGestureRecognizer(target: self, action: #selector(handleTap))
        recognizer.numberOfTapsRequired = 1
        recognizer.cancelsTouchesInView = false
        gestureHost.addGestureRecognizer(recognizer)
        self.recognizer = recognizer
    }

    func update(placements: [ResolvedLobbyCardPlacement]) {
        self.placements = placements
    }

    func updateFocus(viewerPosition: SIMD3<Float>) {
        let focused = LobbyCardFocus.focusedCard(viewerPosition: viewerPosition, placements: placements)
        layer?.setFocused(roomID: focused?.roomID)
    }

    func detach() {
        if let recognizer, let gestureHost {
            gestureHost.removeGestureRecognizer(recognizer)
        }
        recognizer = nil
        layer?.setFocused(roomID: nil)
    }

    @objc private func handleTap(_ gesture: UITapGestureRecognizer) {
        guard gesture.state == .ended, let arView, let layer else { return }
        let point = gesture.location(in: arView)
        guard let tappedRoomID = LobbyCardScreenHitTester.nearestRoomID(
            to: point,
            in: layer,
            projectedBy: arView
        ) else {
            return
        }
        commit(tappedRoomID: tappedRoomID)
    }

    private func commit(tappedRoomID: String) {
        guard let layer else { return }
        guard layer.isFocused(roomID: tappedRoomID) else {
            if let card = placements.first(where: { $0.roomID == tappedRoomID })?.card {
                onDistantCardTapped(card)
            }
            return
        }
        onEnterRoom(tappedRoomID)
    }

    // MARK: - Test seam
    func testTap(roomID: String) {
        commit(tappedRoomID: roomID)
    }
}
