import Foundation

public final class MuseumAPIClient: MuseumServicing, CatalogServicing, MusicCatalogServicing, Sendable {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = NetworkResilience.apiSession()) {
        self.baseURL = baseURL
        self.session = session
    }

    // MARK: - Museum content

    public func createMuseum(accessToken: String, styleID: String) async throws -> Museum {
        let body = try JSONEncoder().encode(CreateMuseumRequestBody(styleID: styleID))
        let decoded: MuseumResponseBody = try await request(method: "POST", path: "museum", accessToken: accessToken, body: body)
        return decoded.model
    }

    public func fetchMuseum(accessToken: String) async throws -> Museum {
        let decoded: MuseumResponseBody = try await request(method: "GET", path: "museum/me", accessToken: accessToken, body: nil)
        return decoded.model
    }

    public func changeStyle(accessToken: String, styleID: String) async throws -> Museum {
        let body = try JSONEncoder().encode(CreateMuseumRequestBody(styleID: styleID))
        let decoded: MuseumResponseBody = try await request(method: "PATCH", path: "museum/me/style", accessToken: accessToken, body: body)
        return decoded.model
    }

    public func changePrivacy(accessToken: String, privacy: MusePrivacy) async throws -> Museum {
        let body = try JSONEncoder().encode(ChangePrivacyRequestBody(privacy: privacy.rawValue))
        let decoded: MuseumResponseBody = try await request(method: "PATCH", path: "museum/me/privacy", accessToken: accessToken, body: body)
        return decoded.model
    }

    public func createRoom(accessToken: String, name: String, variantID: String) async throws -> Room {
        let body = try JSONEncoder().encode(CreateRoomRequestBody(name: name, variantID: variantID))
        let decoded: RoomResponseBody = try await request(method: "POST", path: "museum/me/rooms", accessToken: accessToken, body: body)
        return decoded.model
    }

    public func fetchRoom(accessToken: String, roomID: String) async throws -> Room {
        let decoded: RoomResponseBody = try await request(
            method: "GET",
            path: "museum/me/rooms/\(roomID)",
            accessToken: accessToken,
            body: nil
        )
        return decoded.model
    }

    public func listRooms(accessToken: String) async throws -> [Room] {
        let decoded: RoomListResponseBody = try await request(method: "GET", path: "museum/me/rooms", accessToken: accessToken, body: nil)
        return decoded.rooms.map(\.model)
    }

    public func updateRoom(accessToken: String, roomID: String, patch: RoomPatch) async throws -> Room {
        let body = try JSONEncoder().encode(
            UpdateRoomRequestBody(name: patch.name, variantID: patch.variantID, privacy: patch.privacy?.rawValue)
        )
        let decoded: RoomResponseBody = try await request(
            method: "PATCH",
            path: "museum/me/rooms/\(roomID)",
            accessToken: accessToken,
            body: body
        )
        return decoded.model
    }

    // MARK: - Room music

    public func assignRoomMusic(accessToken: String, roomID: String, musicTrackID: String) async throws -> Room {
        let body = try JSONEncoder().encode(AssignRoomMusicRequestBody(musicTrackID: musicTrackID))
        let decoded: RoomResponseBody = try await request(
            method: "PUT", path: "museum/me/rooms/\(roomID)/music", accessToken: accessToken, body: body)
        return decoded.model
    }

    public func removeRoomMusic(accessToken: String, roomID: String) async throws -> Room {
        let decoded: RoomResponseBody = try await request(
            method: "DELETE", path: "museum/me/rooms/\(roomID)/music", accessToken: accessToken, body: nil)
        return decoded.model
    }

    // MARK: - Music catalog

    public func fetchMusicTracks(accessToken: String) async throws -> [MusicTrack] {
        let decoded: MusicTrackListResponseBody = try await request(
            method: "GET", path: "catalog/music", accessToken: accessToken, body: nil)
        return decoded.tracks.map(\.model)
    }

    public func musicAudioURL(accessToken: String, trackID: String) async throws -> MusicAudioURL {
        let decoded: AudioURLResponseBody = try await request(
            method: "GET", path: "catalog/music/\(trackID)/audio-url", accessToken: accessToken, body: nil)
        return try decoded.model()
    }

    // MARK: - Presentation catalog

    public func fetchStyles(accessToken: String) async throws -> [MuseumStyle] {
        let decoded: StyleListResponseBody = try await request(method: "GET", path: "catalog/styles", accessToken: accessToken, body: nil)
        return decoded.styles.map(\.model)
    }

    public func addSculpture(accessToken: String, roomID: String, catalogID: String) async throws -> [SculptureInstance] {
        let body = try JSONEncoder().encode(AddSculptureRequestBody(catalogID: catalogID))
        let decoded: SculptureListResponseBody = try await request(
            method: "POST", path: "museum/me/rooms/\(roomID)/sculptures", accessToken: accessToken, body: body
        )
        return decoded.sculptures.map(\.model)
    }

    public func removeSculpture(accessToken: String, roomID: String, slotIndex: Int) async throws -> [SculptureInstance] {
        let decoded: SculptureListResponseBody = try await request(
            method: "DELETE", path: "museum/me/rooms/\(roomID)/sculptures/\(slotIndex)", accessToken: accessToken, body: nil
        )
        return decoded.sculptures.map(\.model)
    }

    public func fetchSculptures(accessToken: String) async throws -> [SculptureCatalogEntry] {
        let decoded: SculptureCatalogListResponseBody = try await request(
            method: "GET", path: "catalog/sculptures", accessToken: accessToken, body: nil
        )
        return decoded.sculptures.map(\.model)
    }

    public func fetchVariants(accessToken: String, styleID: String) async throws -> [RoomVariant] {
        let decoded: VariantListResponseBody = try await request(
            method: "GET",
            path: "catalog/styles/\(styleID)/variants",
            accessToken: accessToken,
            body: nil
        )
        return decoded.variants.map(\.model)
    }

    // MARK: - Transport

    private func request<Response: Decodable>(
        method: String,
        path: String,
        accessToken: String,
        body: Data?
    ) async throws -> Response {
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

        guard let httpResponse = response as? HTTPURLResponse else {
            throw IdentityAPIClientError.invalidResponse
        }
        guard (200...299).contains(httpResponse.statusCode) else {
            let errorBody = try? JSONDecoder().decode(MuseumErrorResponseBody.self, from: data)
            throw IdentityAPIClientError.server(statusCode: httpResponse.statusCode, message: errorBody?.error)
        }

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .rfc3339
        return try decoder.decode(Response.self, from: data)
    }
}

// MARK: - Wire types

private struct CreateMuseumRequestBody: Encodable {
    let styleID: String
    enum CodingKeys: String, CodingKey { case styleID = "style_id" }
}

private struct ChangePrivacyRequestBody: Encodable {
    let privacy: String
}

private struct CreateRoomRequestBody: Encodable {
    let name: String
    let variantID: String
    enum CodingKeys: String, CodingKey {
        case name
        case variantID = "variant_id"
    }
}

private struct UpdateRoomRequestBody: Encodable {
    let name: String?
    let variantID: String?
    let privacy: String?

    enum CodingKeys: String, CodingKey {
        case name, privacy
        case variantID = "variant_id"
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(name, forKey: .name)
        try container.encodeIfPresent(variantID, forKey: .variantID)
        try container.encodeIfPresent(privacy, forKey: .privacy)
    }
}

private struct MuseumErrorResponseBody: Decodable {
    let error: String
}

private struct MuseumResponseBody: Decodable {
    let id: String
    let styleID: String
    let privacy: String

    enum CodingKeys: String, CodingKey {
        case id
        case styleID = "style_id"
        case privacy
    }

    var model: Museum {
        Museum(id: id, styleID: styleID, privacy: MusePrivacy(rawValue: privacy) ?? .private)
    }
}

private struct PhotoSlotResponseBody: Decodable {
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

private struct MusicTrackResponseBody: Decodable {
    let id: String
    let displayName: String
    let attribution: String
    let licensing: String
    let durationSeconds: Int

    enum CodingKeys: String, CodingKey {
        case id, attribution, licensing
        case displayName = "display_name"
        case durationSeconds = "duration_seconds"
    }

    var model: MusicTrack {
        MusicTrack(
            id: id,
            displayName: displayName,
            attribution: attribution,
            licensing: MusicTrack.Licensing(rawValue: licensing) ?? .unknown,
            durationSeconds: durationSeconds
        )
    }
}

private struct MusicTrackListResponseBody: Decodable {
    let tracks: [MusicTrackResponseBody]
}

private struct AudioURLResponseBody: Decodable {
    let url: String
    let expiresAt: Date

    enum CodingKeys: String, CodingKey {
        case url
        case expiresAt = "expires_at"
    }

    func model() throws -> MusicAudioURL {
        guard let parsed = URL(string: url) else { throw IdentityAPIClientError.invalidResponse }
        return MusicAudioURL(url: parsed, expiresAt: expiresAt)
    }
}

private struct AssignRoomMusicRequestBody: Encodable {
    let musicTrackID: String
    enum CodingKeys: String, CodingKey { case musicTrackID = "music_track_id" }
}

private struct AddSculptureRequestBody: Encodable {
    let catalogID: String
    enum CodingKeys: String, CodingKey { case catalogID = "catalog_id" }
}

private struct SculptureListResponseBody: Decodable {
    let sculptures: [SculptureResponseBody]
}

private struct SculptureCatalogResponseBody: Decodable {
    let id: String
    let displayName: String
    let assetBundleID: String
    let assetBundleVersion: Int

    enum CodingKeys: String, CodingKey {
        case id
        case displayName = "display_name"
        case assetBundleID = "asset_bundle_id"
        case assetBundleVersion = "asset_bundle_version"
    }

    var model: SculptureCatalogEntry {
        SculptureCatalogEntry(
            id: id,
            displayName: displayName,
            assetBundle: AssetBundleRef(id: assetBundleID, version: assetBundleVersion)
        )
    }
}

private struct SculptureCatalogListResponseBody: Decodable {
    let sculptures: [SculptureCatalogResponseBody]
}

private struct SculptureResponseBody: Decodable {
    let slotIndex: Int
    let catalogID: String

    enum CodingKeys: String, CodingKey {
        case slotIndex = "slot_index"
        case catalogID = "catalog_id"
    }

    var model: SculptureInstance {
        SculptureInstance(slotIndex: slotIndex, catalogID: catalogID)
    }
}

private struct RoomResponseBody: Decodable {
    let id: String
    let name: String
    let variantID: String
    let privacy: String
    let musicTrackID: String?
    let photoSlots: [PhotoSlotResponseBody]
    let sculptures: [SculptureResponseBody]

    enum CodingKeys: String, CodingKey {
        case id, name, privacy
        case variantID = "variant_id"
        case musicTrackID = "music_track_id"
        case photoSlots = "photo_slots"
        case sculptures
    }

    var model: Room {
        Room(
            id: id,
            name: name,
            variantID: variantID,
            privacy: MusePrivacy(rawValue: privacy) ?? .private,
            musicTrackID: musicTrackID,
            photoSlots: photoSlots.map(\.model),
            sculptures: sculptures.map(\.model)
        )
    }
}

private struct RoomListResponseBody: Decodable {
    let rooms: [RoomResponseBody]
}

private struct StyleResponseBody: Decodable {
    let id: String
    let displayName: String
    let assetBundleID: String
    let assetBundleVersion: Int

    enum CodingKeys: String, CodingKey {
        case id
        case displayName = "display_name"
        case assetBundleID = "asset_bundle_id"
        case assetBundleVersion = "asset_bundle_version"
    }

    var model: MuseumStyle {
        MuseumStyle(
            id: id,
            displayName: displayName,
            assetBundle: AssetBundleRef(id: assetBundleID, version: assetBundleVersion)
        )
    }
}

private struct VariantResponseBody: Decodable {
    let id: String
    let styleID: String
    let displayName: String
    let assetBundleID: String
    let assetBundleVersion: Int

    enum CodingKeys: String, CodingKey {
        case id
        case styleID = "style_id"
        case displayName = "display_name"
        case assetBundleID = "asset_bundle_id"
        case assetBundleVersion = "asset_bundle_version"
    }

    var model: RoomVariant {
        RoomVariant(
            id: id,
            styleID: styleID,
            displayName: displayName,
            assetBundle: AssetBundleRef(id: assetBundleID, version: assetBundleVersion)
        )
    }
}

private struct StyleListResponseBody: Decodable {
    let styles: [StyleResponseBody]
}

private struct VariantListResponseBody: Decodable {
    let variants: [VariantResponseBody]
}
