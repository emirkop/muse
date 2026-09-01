import Foundation

public protocol CollectionItemPlacing: Sendable {
    func addItem(
        accessToken: String,
        collectionRoomID: String,
        catalogModelID: String
    ) async throws -> CollectionRoom

    func placeItem(
        accessToken: String,
        collectionRoomID: String,
        collectionItemID: String,
        slotIndex: Int
    ) async throws -> CollectionRoom
}
