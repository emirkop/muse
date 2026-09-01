import Foundation

public protocol CollectionShareLinkServicing: Sendable {
    func ensureCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink

    func currentCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink?

    func regenerateCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink

    func revokeCollectionShareLink(accessToken: String, collectionRoomID: String) async throws
}

public protocol SharedCollectionRoomReading: Sendable {
    func sharedCollectionRoom(accessToken: String, code: String) async throws -> SharedCollectionRoomContent
}
