import Foundation

public protocol RoomPhotoTicketing: Sendable {
    func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket]
}

public protocol RoomPhotoServicing: RoomPhotoTicketing, Sendable {
    func initiateUpload(accessToken: String, declaration: PhotoUploadDeclaration) async throws -> PhotoUploadTicket

    func assignPhotos(accessToken: String, roomID: String, assetIDs: [String]) async throws -> [PhotoSlotAssignment]

    func reorderPhotos(accessToken: String, roomID: String, orderedAssetIDs: [String]) async throws -> [PhotoSlotAssignment]

    func setPhotoCaption(accessToken: String, roomID: String, photoAssetID: String, caption: String) async throws -> [PhotoSlotAssignment]

    func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, replacementAssetID: String) async throws -> [PhotoSlotAssignment]

    func deletePhoto(accessToken: String, roomID: String, photoAssetID: String) async throws -> [PhotoSlotAssignment]
}

public protocol ObjectUploading: Sendable {
    func upload(file: URL, using instructions: PhotoUploadTicket.UploadInstructions) async throws
}
