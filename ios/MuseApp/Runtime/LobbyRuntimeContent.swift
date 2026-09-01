import Foundation

public struct LobbyRuntimeContent: Sendable {
    public enum Geometry: Equatable, Sendable {
        case verificationFixture
    }

    public let museumID: String
    public let styleID: String
    public let geometry: Geometry
    public let viewerRole: MuseumViewerRole
    public let cardTable: LobbyCardTable
    public let placements: [ResolvedLobbyCardPlacement]

    public init(
        museumID: String,
        styleID: String,
        geometry: Geometry,
        viewerRole: MuseumViewerRole,
        cardTable: LobbyCardTable,
        placements: [ResolvedLobbyCardPlacement]
    ) {
        self.museumID = museumID
        self.styleID = styleID
        self.geometry = geometry
        self.viewerRole = viewerRole
        self.cardTable = cardTable
        self.placements = placements
    }

    public var cards: [LobbyRoomCard] { placements.map(\.card) }
}
