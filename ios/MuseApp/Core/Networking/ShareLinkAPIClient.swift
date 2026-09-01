import Foundation

public final class ShareLinkAPIClient: ShareLinkServicing, @unchecked Sendable {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = NetworkResilience.apiSession()) {
        self.baseURL = baseURL
        self.session = session
    }

    // MARK: - Owner

    public func ensureShareLink(accessToken: String) async throws -> MuseumShareLink {
        let decoded: LinkResponseBody = try await request(method: "POST", path: "museum/me/share-link", accessToken: accessToken)
        return try decoded.model()
    }

    public func currentShareLink(accessToken: String) async throws -> MuseumShareLink? {
        do {
            let decoded: LinkResponseBody = try await request(method: "GET", path: "museum/me/share-link", accessToken: accessToken)
            return try decoded.model()
        } catch IdentityAPIClientError.server(let statusCode, _) where statusCode == 404 {
            return nil
        }
    }

    public func regenerateShareLink(accessToken: String) async throws -> MuseumShareLink {
        let decoded: LinkResponseBody = try await request(method: "POST", path: "museum/me/share-link/regenerate", accessToken: accessToken)
        return try decoded.model()
    }

    // MARK: - Visitor

    public func preview(code: String) async throws -> ShareLinkPreview {
        let decoded: PreviewResponseBody = try await request(method: "GET", path: "share-links/\(code)", accessToken: nil)
        return decoded.model
    }

    public func sharedMuseum(accessToken: String, code: String) async throws -> SharedMuseumContent {
        let decoded: SharedMuseumResponseBody = try await request(method: "GET", path: "share-links/\(code)/museum", accessToken: accessToken)
        return decoded.model
    }

    public func sharedRoom(accessToken: String, code: String, roomID: String) async throws -> SharedRoomContent {
        let decoded: SharedRoomResponseBody = try await request(
            method: "GET", path: "share-links/\(code)/rooms/\(roomID)", accessToken: accessToken)
        return decoded.model
    }

    public func sharedRoomPhotoURLs(accessToken: String, code: String, roomID: String) async throws -> [PhotoDownloadTicket] {
        let decoded: PhotoTicketsResponseBody = try await request(
            method: "GET", path: "share-links/\(code)/rooms/\(roomID)/photo-urls", accessToken: accessToken)
        return decoded.model
    }

    // MARK: - Transport

    private func request<Response: Decodable>(method: String, path: String, accessToken: String?) async throws -> Response {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = method
        if let accessToken {
            request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.resilientData(for: request)
        } catch {
            throw IdentityAPIClientError.classify(error)
        }
        guard let httpResponse = response as? HTTPURLResponse else {
            throw IdentityAPIClientError.invalidResponse
        }
        guard (200...299).contains(httpResponse.statusCode) else {
            let errorBody = try? JSONDecoder().decode(ErrorResponseBody.self, from: data)
            throw IdentityAPIClientError.server(statusCode: httpResponse.statusCode, message: errorBody?.error)
        }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .rfc3339
        return try decoder.decode(Response.self, from: data)
    }
}

// MARK: - Wire types

private struct LinkResponseBody: Decodable {
    let code: String
    let url: String
    let createdAt: Date

    enum CodingKeys: String, CodingKey {
        case code, url
        case createdAt = "created_at"
    }

    func model() throws -> MuseumShareLink {
        guard let parsed = URL(string: url) else { throw IdentityAPIClientError.invalidResponse }
        return MuseumShareLink(code: code, url: parsed, createdAt: createdAt)
    }
}

private struct PreviewResponseBody: Decodable {
    let code: String
    let styleID: String
    let owner: OwnerBody

    struct OwnerBody: Decodable {
        let avatarID: String
        enum CodingKeys: String, CodingKey {
            case avatarID = "avatar_id"
        }
    }

    enum CodingKeys: String, CodingKey {
        case code, owner
        case styleID = "style_id"
    }

    var model: ShareLinkPreview {
        ShareLinkPreview(code: code, styleID: styleID, ownerAvatarID: owner.avatarID)
    }
}

private struct SharedMuseumResponseBody: Decodable {
    let museumID: String
    let styleID: String
    let rooms: [RoomBody]

    struct RoomBody: Decodable {
        let id: String
        let name: String
        let variantID: String
        enum CodingKeys: String, CodingKey {
            case id, name
            case variantID = "variant_id"
        }
    }

    enum CodingKeys: String, CodingKey {
        case rooms
        case museumID = "museum_id"
        case styleID = "style_id"
    }

    var model: SharedMuseumContent {
        SharedMuseumContent(
            museumID: museumID,
            styleID: styleID,
            rooms: rooms.map { SharedRoomSummary(id: $0.id, name: $0.name, variantID: $0.variantID) }
        )
    }
}

private struct SharedRoomResponseBody: Decodable {
    let id: String
    let name: String
    let variantID: String
    let musicTrackID: String?
    let photoSlots: [SlotBody]
    let sculptures: [SculptureBody]

    struct SlotBody: Decodable {
        let slotIndex: Int
        let photoAssetID: String
        let caption: String
        enum CodingKeys: String, CodingKey {
            case slotIndex = "slot_index"
            case photoAssetID = "photo_asset_id"
            case caption
        }
    }

    struct SculptureBody: Decodable {
        let slotIndex: Int
        let catalogID: String
        enum CodingKeys: String, CodingKey {
            case slotIndex = "slot_index"
            case catalogID = "catalog_id"
        }
    }

    enum CodingKeys: String, CodingKey {
        case id, name, sculptures
        case variantID = "variant_id"
        case musicTrackID = "music_track_id"
        case photoSlots = "photo_slots"
    }

    var model: SharedRoomContent {
        SharedRoomContent(
            id: id,
            name: name,
            variantID: variantID,
            musicTrackID: musicTrackID,
            photoSlots: photoSlots.map {
                PhotoSlotAssignment(slotIndex: $0.slotIndex, photoAssetID: $0.photoAssetID, caption: $0.caption)
            },
            sculptures: sculptures.map {
                SculptureInstance(slotIndex: $0.slotIndex, catalogID: $0.catalogID)
            }
        )
    }
}

private struct PhotoTicketsResponseBody: Decodable {
    let tickets: [TicketBody]

    struct TicketBody: Decodable {
        let photoAssetID: String
        let url: URL
        let expiresAt: Date
        let pixelWidth: Int
        let pixelHeight: Int

        enum CodingKeys: String, CodingKey {
            case url
            case photoAssetID = "photo_asset_id"
            case expiresAt = "expires_at"
            case pixelWidth = "pixel_width"
            case pixelHeight = "pixel_height"
        }
    }

    var model: [PhotoDownloadTicket] {
        tickets.map {
            PhotoDownloadTicket(
                photoAssetID: $0.photoAssetID,
                url: $0.url,
                expiresAt: $0.expiresAt,
                pixelWidth: $0.pixelWidth,
                pixelHeight: $0.pixelHeight
            )
        }
    }
}

private struct ErrorResponseBody: Decodable {
    let error: String
}
