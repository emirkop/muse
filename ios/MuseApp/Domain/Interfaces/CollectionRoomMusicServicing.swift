import Foundation

public protocol CollectionRoomMusicServicing: Sendable {
    func assignCollectionRoomMusic(accessToken: String, collectionRoomID: String, musicTrackID: String) async throws -> CollectionRoom

    func removeCollectionRoomMusic(accessToken: String, collectionRoomID: String) async throws -> CollectionRoom
}
