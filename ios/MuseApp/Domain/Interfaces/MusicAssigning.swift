import Foundation

public protocol MusicAssigning: Sendable {
    func assignMusic(trackID: String) async throws -> String?
    func removeMusic() async throws -> String?
}

public struct MuseumRoomMusicAssignment: MusicAssigning {
    private let museumService: any MuseumServicing
    private let accessToken: String
    private let roomID: String

    public init(museumService: any MuseumServicing, accessToken: String, roomID: String) {
        self.museumService = museumService
        self.accessToken = accessToken
        self.roomID = roomID
    }

    public func assignMusic(trackID: String) async throws -> String? {
        try await museumService.assignRoomMusic(accessToken: accessToken, roomID: roomID, musicTrackID: trackID).musicTrackID
    }

    public func removeMusic() async throws -> String? {
        try await museumService.removeRoomMusic(accessToken: accessToken, roomID: roomID).musicTrackID
    }
}

public struct CollectionRoomMusicAssignment: MusicAssigning {
    private let service: any CollectionRoomMusicServicing
    private let accessToken: String
    private let collectionRoomID: String

    public init(service: any CollectionRoomMusicServicing, accessToken: String, collectionRoomID: String) {
        self.service = service
        self.accessToken = accessToken
        self.collectionRoomID = collectionRoomID
    }

    public func assignMusic(trackID: String) async throws -> String? {
        try await service.assignCollectionRoomMusic(
            accessToken: accessToken, collectionRoomID: collectionRoomID, musicTrackID: trackID
        ).musicTrackID
    }

    public func removeMusic() async throws -> String? {
        try await service.removeCollectionRoomMusic(accessToken: accessToken, collectionRoomID: collectionRoomID).musicTrackID
    }
}
