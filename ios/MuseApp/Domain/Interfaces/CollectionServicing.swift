import Foundation

public protocol CollectionServicing: Sendable {
    func createCollectionRoom(
        accessToken: String,
        name: String,
        categoryID: String?,
        designID: String?
    ) async throws -> CollectionRoom

    func listCollectionRooms(accessToken: String) async throws -> [CollectionRoom]

    func fetchCollectionRoom(accessToken: String, collectionRoomID: String) async throws -> CollectionRoom

    func updateCollectionRoom(
        accessToken: String,
        collectionRoomID: String,
        patch: CollectionRoomPatch
    ) async throws -> CollectionRoom

    func deleteCollectionRoom(accessToken: String, collectionRoomID: String) async throws
}
