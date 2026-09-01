import Foundation

public protocol ShareLinkServicing: Sendable {
    // MARK: Owner

    func ensureShareLink(accessToken: String) async throws -> MuseumShareLink

    func currentShareLink(accessToken: String) async throws -> MuseumShareLink?

    func regenerateShareLink(accessToken: String) async throws -> MuseumShareLink

    // MARK: Visitor

    func preview(code: String) async throws -> ShareLinkPreview

    func sharedMuseum(accessToken: String, code: String) async throws -> SharedMuseumContent

    func sharedRoom(accessToken: String, code: String, roomID: String) async throws -> SharedRoomContent

    func sharedRoomPhotoURLs(accessToken: String, code: String, roomID: String) async throws -> [PhotoDownloadTicket]
}

public struct SharedRoomPhotoTickets: RoomPhotoTicketing {
    private let shareLinkService: any ShareLinkServicing
    private let code: String

    public init(shareLinkService: any ShareLinkServicing, code: String) {
        self.shareLinkService = shareLinkService
        self.code = code
    }

    public func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] {
        try await shareLinkService.sharedRoomPhotoURLs(accessToken: accessToken, code: code, roomID: roomID)
    }
}
