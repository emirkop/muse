import Foundation

public protocol MuseumServicing: Sendable {
    func createMuseum(accessToken: String, styleID: String) async throws -> Museum

    func fetchMuseum(accessToken: String) async throws -> Museum

    func changeStyle(accessToken: String, styleID: String) async throws -> Museum

    func changePrivacy(accessToken: String, privacy: MusePrivacy) async throws -> Museum

    func createRoom(accessToken: String, name: String, variantID: String) async throws -> Room

    func listRooms(accessToken: String) async throws -> [Room]

    func fetchRoom(accessToken: String, roomID: String) async throws -> Room

    func updateRoom(accessToken: String, roomID: String, patch: RoomPatch) async throws -> Room

    func addSculpture(accessToken: String, roomID: String, catalogID: String) async throws -> [SculptureInstance]

    func removeSculpture(accessToken: String, roomID: String, slotIndex: Int) async throws -> [SculptureInstance]

    func assignRoomMusic(accessToken: String, roomID: String, musicTrackID: String) async throws -> Room

    func removeRoomMusic(accessToken: String, roomID: String) async throws -> Room
}

public protocol CatalogServicing: Sendable {
    func fetchStyles(accessToken: String) async throws -> [MuseumStyle]

    func fetchVariants(accessToken: String, styleID: String) async throws -> [RoomVariant]

    func fetchSculptures(accessToken: String) async throws -> [SculptureCatalogEntry]
}

public struct MuseumLobbyContent: Equatable, Sendable {
    public let museumID: String
    public let styleID: String
    public let rooms: [Room]

    public init(museumID: String, styleID: String, rooms: [Room]) {
        self.museumID = museumID
        self.styleID = styleID
        self.rooms = rooms
    }
}

public protocol MuseumLobbyContentProviding: Sendable {
    func lobbyContent(accessToken: String) async throws -> MuseumLobbyContent
}

public struct OwnedMuseumLobbyContent: MuseumLobbyContentProviding {
    private let museumService: any MuseumServicing

    public init(museumService: any MuseumServicing) {
        self.museumService = museumService
    }

    public func lobbyContent(accessToken: String) async throws -> MuseumLobbyContent {
        let museum = try await museumService.fetchMuseum(accessToken: accessToken)
        let rooms = try await museumService.listRooms(accessToken: accessToken)
        return MuseumLobbyContent(museumID: museum.id, styleID: museum.styleID, rooms: rooms)
    }
}

public struct SharedMuseumLobbyContent: MuseumLobbyContentProviding {
    private let shareLinkService: any ShareLinkServicing
    private let code: String

    public init(shareLinkService: any ShareLinkServicing, code: String) {
        self.shareLinkService = shareLinkService
        self.code = code
    }

    public func lobbyContent(accessToken: String) async throws -> MuseumLobbyContent {
        let shared = try await shareLinkService.sharedMuseum(accessToken: accessToken, code: code)
        return MuseumLobbyContent(
            museumID: shared.museumID,
            styleID: shared.styleID,
            rooms: shared.rooms.map {
                Room(id: $0.id, name: $0.name, variantID: $0.variantID, privacy: .public)
            }
        )
    }
}
