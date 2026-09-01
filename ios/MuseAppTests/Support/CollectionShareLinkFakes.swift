import Foundation
@testable import MuseApp

final class FakeCollectionShareLinkService: CollectionShareLinkServicing, SharedCollectionRoomReading, @unchecked Sendable {
    private(set) var activeLinks: [String: CollectionRoomShareLink] = [:]
    private(set) var deadCodes: Set<String> = []
    var sharedRooms: [String: SharedCollectionRoomContent] = [:]

    var ownerFailure: Error?
    var visitFailure: Error?

    private(set) var ensureCallCount = 0
    private(set) var regenerateCallCount = 0
    private(set) var revokeCallCount = 0
    private(set) var visitCallCount = 0
    private var minted = 0

    private func mint(roomID: String) -> CollectionRoomShareLink {
        minted += 1
        let code = String(format: "%@%016d", "c-", minted).padding(toLength: 22, withPad: "x", startingAt: 0)
        return CollectionRoomShareLink(
            collectionRoomID: roomID,
            code: code,
            url: URL(string: "https://muse.app/c/\(code)")!,
            createdAt: Date(timeIntervalSince1970: 1_700_000_000)
        )
    }

    func ensureCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink {
        ensureCallCount += 1
        if let ownerFailure { throw ownerFailure }
        if let existing = activeLinks[collectionRoomID] { return existing }
        let link = mint(roomID: collectionRoomID)
        activeLinks[collectionRoomID] = link
        return link
    }

    func currentCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink? {
        if let ownerFailure { throw ownerFailure }
        return activeLinks[collectionRoomID]
    }

    func regenerateCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink {
        regenerateCallCount += 1
        if let ownerFailure { throw ownerFailure }
        if let old = activeLinks[collectionRoomID] { deadCodes.insert(old.code) }
        let link = mint(roomID: collectionRoomID)
        activeLinks[collectionRoomID] = link
        return link
    }

    func revokeCollectionShareLink(accessToken: String, collectionRoomID: String) async throws {
        revokeCallCount += 1
        if let ownerFailure { throw ownerFailure }
        if let old = activeLinks.removeValue(forKey: collectionRoomID) { deadCodes.insert(old.code) }
    }

    func sharedCollectionRoom(accessToken: String, code: String) async throws -> SharedCollectionRoomContent {
        visitCallCount += 1
        if let visitFailure { throw visitFailure }
        if let content = sharedRooms[code], !deadCodes.contains(code) { return content }
        throw CollectionAPIError(statusCode: 404, code: nil, message: "not found")
    }
}
