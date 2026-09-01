import RealityKit
import UIKit

public final class RealityKitMuseumRuntime: MuseumRuntimeInterface {
    private let diagnostics: any ErrorReporting

    public init(diagnostics: any ErrorReporting = NoErrorReporting()) {
        self.diagnostics = diagnostics
    }

    public func makeRuntimeViewController() -> UIViewController {
        RealityKitSceneViewController(content: nil)
    }

    public func makeRoomViewController(content: RoomRuntimeContent) -> UIViewController {
        RealityKitSceneViewController(content: content, diagnostics: diagnostics)
    }

    public func makeLobbyViewController(
        content: LobbyRuntimeContent,
        onEnterRoom: @MainActor @escaping (String) -> Void
    ) -> UIViewController {
        RealityKitLobbyViewController(content: content, onEnterRoom: onEnterRoom)
    }
}
