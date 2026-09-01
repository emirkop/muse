import UIKit

@MainActor
public protocol MuseumRuntimeInterface {
    func makeRuntimeViewController() -> UIViewController

    func makeRoomViewController(content: RoomRuntimeContent) -> UIViewController

    func makeLobbyViewController(
        content: LobbyRuntimeContent,
        onEnterRoom: @MainActor @escaping (String) -> Void
    ) -> UIViewController
}

public protocol LobbyGeometryProviding: Sendable {
    func lobbyGeometry(forStyleID styleID: String) async -> LobbyRuntimeContent.Geometry?
}

public struct UnavailableLobbyGeometryProvider: LobbyGeometryProviding {
    public init() {}

    public func lobbyGeometry(forStyleID styleID: String) async -> LobbyRuntimeContent.Geometry? {
        nil
    }
}

public enum SculptureModelSource: Equatable, Sendable {
    case verificationFixture
}

public protocol SculptureModelProviding: Sendable {
    func modelSource(forCatalogID catalogID: String) async -> SculptureModelSource?
}

public struct UnavailableSculptureModelProvider: SculptureModelProviding {
    public init() {}

    public func modelSource(forCatalogID catalogID: String) async -> SculptureModelSource? {
        nil
    }
}
