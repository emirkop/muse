import Foundation
@testable import MuseApp

final class FakeShareLinkService: ShareLinkServicing, @unchecked Sendable {
    var activeLink: MuseumShareLink?
    private var minted = 0

    var ensureResult: Result<MuseumShareLink, Error>?
    var regenerateResult: Result<MuseumShareLink, Error>?
    var previewResult: Result<ShareLinkPreview, Error> = .failure(IdentityAPIClientError.server(statusCode: 404, message: "not found"))
    var sharedMuseumResult: Result<SharedMuseumContent, Error> = .failure(IdentityAPIClientError.server(statusCode: 404, message: "not found"))
    var sharedRooms: [String: SharedRoomContent] = [:]
    var sharedRoomTickets: [String: [PhotoDownloadTicket]] = [:]

    private(set) var ensureCallCount = 0
    private(set) var currentCallCount = 0
    private(set) var regenerateCallCount = 0
    private(set) var previewedCodes: [String] = []
    private(set) var sharedMuseumRequests: [(token: String, code: String)] = []
    private(set) var sharedRoomRequests: [(code: String, roomID: String)] = []
    private(set) var sharedRoomTicketRequests: [(code: String, roomID: String)] = []

    static func link(_ code: String) -> MuseumShareLink {
        MuseumShareLink(code: code, url: URL(string: "https://muse.app/m/\(code)")!, createdAt: Date(timeIntervalSince1970: 0))
    }

    private func mint() -> MuseumShareLink {
        minted += 1
        return Self.link(String(repeating: "L", count: 21) + String(minted))
    }

    func ensureShareLink(accessToken: String) async throws -> MuseumShareLink {
        ensureCallCount += 1
        if let ensureResult { return try ensureResult.get() }
        if let activeLink { return activeLink }
        let created = mint()
        activeLink = created
        return created
    }

    func currentShareLink(accessToken: String) async throws -> MuseumShareLink? {
        currentCallCount += 1
        return activeLink
    }

    func regenerateShareLink(accessToken: String) async throws -> MuseumShareLink {
        regenerateCallCount += 1
        if let regenerateResult { return try regenerateResult.get() }
        let created = mint()
        activeLink = created
        return created
    }

    func preview(code: String) async throws -> ShareLinkPreview {
        previewedCodes.append(code)
        return try previewResult.get()
    }

    func sharedMuseum(accessToken: String, code: String) async throws -> SharedMuseumContent {
        sharedMuseumRequests.append((token: accessToken, code: code))
        return try sharedMuseumResult.get()
    }

    func sharedRoom(accessToken: String, code: String, roomID: String) async throws -> SharedRoomContent {
        sharedRoomRequests.append((code: code, roomID: roomID))
        guard let room = sharedRooms[roomID] else {
            throw IdentityAPIClientError.server(statusCode: 404, message: "not found")
        }
        return room
    }

    func sharedRoomPhotoURLs(accessToken: String, code: String, roomID: String) async throws -> [PhotoDownloadTicket] {
        sharedRoomTicketRequests.append((code: code, roomID: roomID))
        guard let tickets = sharedRoomTickets[roomID] else {
            throw IdentityAPIClientError.server(statusCode: 404, message: "not found")
        }
        return tickets
    }
}
