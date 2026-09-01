import Foundation

public final class PhotoUploadAPIClient: RoomPhotoServicing, Sendable {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = NetworkResilience.apiSession()) {
        self.baseURL = baseURL
        self.session = session
    }

    public func initiateUpload(accessToken: String, declaration: PhotoUploadDeclaration) async throws -> PhotoUploadTicket {
        let body = try JSONEncoder().encode(InitiateUploadRequestBody(declaration))
        let decoded: InitiateUploadResponseBody = try await request(method: "POST", path: "media/photo-uploads", accessToken: accessToken, body: body)
        return try decoded.model()
    }

    public func assignPhotos(accessToken: String, roomID: String, assetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        let body = try JSONEncoder().encode(AssignPhotosRequestBody(assetIDs: assetIDs))
        let decoded: AssignPhotosResponseBody = try await request(method: "POST", path: "museum/me/rooms/\(roomID)/photos", accessToken: accessToken, body: body)
        return decoded.photoSlots.map(\.model)
    }

    public func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] {
        let decoded: PhotoURLsResponseBody = try await request(method: "GET", path: "museum/me/rooms/\(roomID)/photo-urls", accessToken: accessToken, body: nil)
        return try decoded.tickets.map { try $0.model() }
    }

    public func reorderPhotos(accessToken: String, roomID: String, orderedAssetIDs: [String]) async throws -> [PhotoSlotAssignment] {
        let body = try JSONEncoder().encode(ReorderPhotosRequestBody(photoAssetIDs: orderedAssetIDs))
        let decoded: AssignPhotosResponseBody = try await request(method: "PUT", path: "museum/me/rooms/\(roomID)/photo-order", accessToken: accessToken, body: body)
        return decoded.photoSlots.map(\.model)
    }

    public func setPhotoCaption(accessToken: String, roomID: String, photoAssetID: String, caption: String) async throws -> [PhotoSlotAssignment] {
        let body = try JSONEncoder().encode(SetCaptionRequestBody(caption: caption))
        let decoded: AssignPhotosResponseBody = try await request(
            method: "PUT",
            path: "museum/me/rooms/\(roomID)/photos/\(photoAssetID)/caption",
            accessToken: accessToken,
            body: body
        )
        return decoded.photoSlots.map(\.model)
    }

    public func replacePhoto(accessToken: String, roomID: String, photoAssetID: String, replacementAssetID: String) async throws -> [PhotoSlotAssignment] {
        let body = try JSONEncoder().encode(ReplacePhotoRequestBody(assetID: replacementAssetID))
        let decoded: AssignPhotosResponseBody = try await request(
            method: "POST",
            path: "museum/me/rooms/\(roomID)/photos/\(photoAssetID)/replacement",
            accessToken: accessToken,
            body: body
        )
        return decoded.photoSlots.map(\.model)
    }

    public func deletePhoto(accessToken: String, roomID: String, photoAssetID: String) async throws -> [PhotoSlotAssignment] {
        let decoded: AssignPhotosResponseBody = try await request(
            method: "DELETE",
            path: "museum/me/rooms/\(roomID)/photos/\(photoAssetID)",
            accessToken: accessToken,
            body: nil
        )
        return decoded.photoSlots.map(\.model)
    }

    // MARK: - Transport

    private func request<Response: Decodable>(method: String, path: String, accessToken: String, body: Data?) async throws -> Response {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = method
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = body
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.resilientData(for: request)
        } catch {
            throw IdentityAPIClientError.classify(error)
        }
        guard let http = response as? HTTPURLResponse else { throw IdentityAPIClientError.invalidResponse }
        guard (200...299).contains(http.statusCode) else {
            let errorBody = try? Self.decoder.decode(PhotoErrorResponseBody.self, from: data)
            throw PhotoAPIError(statusCode: http.statusCode, message: errorBody?.error, code: errorBody?.code, assetID: errorBody?.assetID)
        }
        return try Self.decoder.decode(Response.self, from: data)
    }

    private static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .rfc3339
        return decoder
    }()
}

public struct PhotoAPIError: Error, Equatable, Sendable {
    public let statusCode: Int
    public let message: String?
    public let code: String?
    public let assetID: String?

    public init(statusCode: Int, message: String?, code: String?, assetID: String?) {
        self.statusCode = statusCode
        self.message = message
        self.code = code
        self.assetID = assetID
    }
}

public struct URLSessionObjectUploader: ObjectUploading {
    private let session: URLSession

    public init(session: URLSession = NetworkResilience.uploadSession()) {
        self.session = session
    }

    public func upload(file: URL, using instructions: PhotoUploadTicket.UploadInstructions) async throws {
        var request = URLRequest(url: instructions.url)
        request.httpMethod = instructions.method
        for (name, value) in instructions.headers {
            request.setValue(value, forHTTPHeaderField: name)
        }
        let (_, response) = try await session.upload(for: request, fromFile: file)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PhotoAPIError(statusCode: (response as? HTTPURLResponse)?.statusCode ?? -1, message: "storage refused the upload", code: nil, assetID: nil)
        }
    }
}

// MARK: - Wire types

private struct InitiateUploadRequestBody: Encodable {
    let clientUploadID: String
    let contentType: String
    let byteSize: Int
    let pixelWidth: Int
    let pixelHeight: Int
    let checksumSHA256: String

    init(_ d: PhotoUploadDeclaration) {
        clientUploadID = d.clientUploadID
        contentType = d.contentType
        byteSize = d.byteSize
        pixelWidth = d.pixelWidth
        pixelHeight = d.pixelHeight
        checksumSHA256 = d.sha256Hex
    }

    enum CodingKeys: String, CodingKey {
        case clientUploadID = "client_upload_id"
        case contentType = "content_type"
        case byteSize = "byte_size"
        case pixelWidth = "pixel_width"
        case pixelHeight = "pixel_height"
        case checksumSHA256 = "checksum_sha256"
    }
}

private struct InitiateUploadResponseBody: Decodable {
    struct Upload: Decodable {
        let url: String
        let method: String
        let headers: [String: String]
        let expiresAt: Date

        enum CodingKeys: String, CodingKey {
            case url, method, headers
            case expiresAt = "expires_at"
        }
    }

    let assetID: String
    let state: String
    let upload: Upload?

    enum CodingKeys: String, CodingKey {
        case assetID = "asset_id"
        case state, upload
    }

    func model() throws -> PhotoUploadTicket {
        var instructions: PhotoUploadTicket.UploadInstructions?
        if let upload {
            guard let url = URL(string: upload.url) else { throw IdentityAPIClientError.invalidResponse }
            instructions = .init(url: url, method: upload.method, headers: upload.headers, expiresAt: upload.expiresAt)
        }
        return PhotoUploadTicket(assetID: assetID, isCommitted: state == "committed", upload: instructions)
    }
}

private struct AssignPhotosRequestBody: Encodable {
    let assetIDs: [String]
    enum CodingKeys: String, CodingKey { case assetIDs = "asset_ids" }
}

private struct ReorderPhotosRequestBody: Encodable {
    let photoAssetIDs: [String]
    enum CodingKeys: String, CodingKey { case photoAssetIDs = "photo_asset_ids" }
}

private struct SetCaptionRequestBody: Encodable {
    let caption: String
}

private struct ReplacePhotoRequestBody: Encodable {
    let assetID: String
    enum CodingKeys: String, CodingKey { case assetID = "asset_id" }
}

private struct PhotoSlotBody: Decodable {
    let slotIndex: Int
    let photoAssetID: String
    let caption: String

    enum CodingKeys: String, CodingKey {
        case slotIndex = "slot_index"
        case photoAssetID = "photo_asset_id"
        case caption
    }

    var model: PhotoSlotAssignment {
        PhotoSlotAssignment(slotIndex: slotIndex, photoAssetID: photoAssetID, caption: caption)
    }
}

private struct AssignPhotosResponseBody: Decodable {
    let photoSlots: [PhotoSlotBody]
    enum CodingKeys: String, CodingKey { case photoSlots = "photo_slots" }
}

private struct PhotoURLBody: Decodable {
    let photoAssetID: String
    let url: String
    let expiresAt: Date
    let pixelWidth: Int
    let pixelHeight: Int

    enum CodingKeys: String, CodingKey {
        case photoAssetID = "photo_asset_id"
        case url
        case expiresAt = "expires_at"
        case pixelWidth = "pixel_width"
        case pixelHeight = "pixel_height"
    }

    func model() throws -> PhotoDownloadTicket {
        guard let parsed = URL(string: url) else { throw IdentityAPIClientError.invalidResponse }
        return PhotoDownloadTicket(photoAssetID: photoAssetID, url: parsed, expiresAt: expiresAt, pixelWidth: pixelWidth, pixelHeight: pixelHeight)
    }
}

private struct PhotoURLsResponseBody: Decodable {
    let tickets: [PhotoURLBody]
}

private struct PhotoErrorResponseBody: Decodable {
    let error: String
    let code: String?
    let assetID: String?

    enum CodingKeys: String, CodingKey {
        case error, code
        case assetID = "asset_id"
    }
}
