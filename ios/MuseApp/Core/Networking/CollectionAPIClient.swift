import Foundation

public final class CollectionAPIClient:
    CollectionServicing,
    CollectionCatalogServicing,
    CollectionTierRatcheting,
    CollectionItemPlacing,
    CollectionPresentationAssetReading,
    CollectionShareLinkServicing,
    SharedCollectionRoomReading,
    CollectionRoomMusicServicing,
    Sendable {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = NetworkResilience.apiSession()) {
        self.baseURL = baseURL
        self.session = session
    }

    // MARK: - Collection Catalog

    public func fetchCollectionCategories(accessToken: String) async throws -> [CollectionCategory] {
        let decoded: CollectionCategoryListBody = try await request(
            method: "GET", path: "catalog/collection-categories", accessToken: accessToken, body: nil
        )
        return decoded.collectionCategories.map(\.model)
    }

    public func fetchCollectionDesigns(
        accessToken: String,
        categoryID: String
    ) async throws -> [CollectionDesign] {
        let decoded: CollectionDesignListBody = try await request(
            method: "GET",
            path: "catalog/collection-designs",
            query: ["category_id": categoryID],
            accessToken: accessToken,
            body: nil
        )
        return decoded.collectionDesigns.map(\.model)
    }

    public func searchCollectionModels(
        accessToken: String,
        categoryID: String,
        query: String,
        limit: Int,
        cursor: CollectionModelSearchCursor?
    ) async throws -> CollectionModelSearchPage {
        var parameters = ["category_id": categoryID]
        if !query.isEmpty { parameters["q"] = query }
        if limit > 0 { parameters["limit"] = String(limit) }
        if let cursor {
            parameters["cursor_name"] = cursor.displayName
            parameters["cursor_id"] = cursor.id
        }

        let decoded: ModelSearchPageBody = try await request(
            method: "GET",
            path: "catalog/collection-models",
            query: parameters,
            accessToken: accessToken,
            body: nil
        )
        return decoded.model
    }

    // MARK: - Presentation asset mapping

    public func fetchPresentationAssets(
        accessToken: String,
        catalogModelIDs: [String]
    ) async throws -> [CollectionPresentationAssetEntry] {
        guard !catalogModelIDs.isEmpty else { return [] }
        let decoded: PresentationAssetsBody = try await request(
            method: "GET",
            path: "catalog/collection-presentation-assets",
            query: ["model_ids": catalogModelIDs.joined(separator: ",")],
            accessToken: accessToken,
            body: nil
        )
        return decoded.assets.map(\.entry)
    }

    // MARK: - Collection content

    public func createCollectionRoom(
        accessToken: String,
        name: String,
        categoryID: String?,
        designID: String?
    ) async throws -> CollectionRoom {
        let body = try JSONEncoder().encode(
            CreateCollectionRoomBody(name: name, categoryID: categoryID, designID: designID)
        )
        let decoded: CollectionRoomBody = try await request(
            method: "POST", path: "collection-rooms", accessToken: accessToken, body: body
        )
        return decoded.model
    }

    public func listCollectionRooms(accessToken: String) async throws -> [CollectionRoom] {
        let decoded: CollectionRoomListBody = try await request(
            method: "GET", path: "collection-rooms", accessToken: accessToken, body: nil
        )
        return decoded.collectionRooms.map(\.model)
    }

    public func fetchCollectionRoom(accessToken: String, collectionRoomID: String) async throws -> CollectionRoom {
        let decoded: CollectionRoomBody = try await request(
            method: "GET", path: "collection-rooms/\(collectionRoomID)", accessToken: accessToken, body: nil
        )
        return decoded.model
    }

    public func updateCollectionRoom(
        accessToken: String,
        collectionRoomID: String,
        patch: CollectionRoomPatch
    ) async throws -> CollectionRoom {
        let body = try JSONEncoder().encode(
            UpdateCollectionRoomBody(name: patch.name, categoryID: patch.categoryID, designID: patch.designID)
        )
        let decoded: CollectionRoomBody = try await request(
            method: "PATCH", path: "collection-rooms/\(collectionRoomID)", accessToken: accessToken, body: body
        )
        return decoded.model
    }

    // MARK: - Tier expansion

    public func ratchetTier(
        accessToken: String,
        collectionRoomID: String,
        tier: CollectionTier
    ) async throws -> CollectionRoom {
        let body = try JSONEncoder().encode(RatchetTierBody(tier: tier.ordinal))
        let decoded: CollectionRoomBody = try await request(
            method: "POST",
            path: "collection-rooms/\(collectionRoomID)/tier",
            accessToken: accessToken,
            body: body
        )
        return decoded.model
    }

    // MARK: - Item placement and reordering

    public func addItem(
        accessToken: String,
        collectionRoomID: String,
        catalogModelID: String
    ) async throws -> CollectionRoom {
        let body = try JSONEncoder().encode(AddCollectionItemBody(catalogModelID: catalogModelID))
        let decoded: CollectionRoomBody = try await request(
            method: "POST",
            path: "collection-rooms/\(collectionRoomID)/items",
            query: Self.assetVersionQuery,
            accessToken: accessToken,
            body: body
        )
        return decoded.model
    }

    public func placeItem(
        accessToken: String,
        collectionRoomID: String,
        collectionItemID: String,
        slotIndex: Int
    ) async throws -> CollectionRoom {
        let body = try JSONEncoder().encode(PlaceCollectionItemBody(slotIndex: slotIndex))
        let decoded: CollectionRoomBody = try await request(
            method: "PUT",
            path: "collection-rooms/\(collectionRoomID)/items/\(collectionItemID)/slot",
            query: Self.assetVersionQuery,
            accessToken: accessToken,
            body: body
        )
        return decoded.model
    }

    private static var assetVersionQuery: [String: String] {
        ["app_asset_version": String(AssetBundleFormat.appAssetVersion)]
    }

    public func deleteCollectionRoom(accessToken: String, collectionRoomID: String) async throws {
        try await requestExpectingNoContent(
            method: "DELETE", path: "collection-rooms/\(collectionRoomID)", accessToken: accessToken
        )
    }

    // MARK: - Music

    public func assignCollectionRoomMusic(accessToken: String, collectionRoomID: String, musicTrackID: String) async throws -> CollectionRoom {
        let body = try JSONEncoder().encode(AssignCollectionMusicRequestBody(musicTrackID: musicTrackID))
        let decoded: CollectionRoomBody = try await request(
            method: "PUT", path: "collection-rooms/\(collectionRoomID)/music", accessToken: accessToken, body: body
        )
        return decoded.model
    }

    public func removeCollectionRoomMusic(accessToken: String, collectionRoomID: String) async throws -> CollectionRoom {
        let decoded: CollectionRoomBody = try await request(
            method: "DELETE", path: "collection-rooms/\(collectionRoomID)/music", accessToken: accessToken, body: nil
        )
        return decoded.model
    }

    // MARK: - Sharing

    public func ensureCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink {
        let decoded: CollectionShareLinkBody = try await request(
            method: "POST", path: "collection-rooms/\(collectionRoomID)/share-link", accessToken: accessToken, body: nil
        )
        return try decoded.model()
    }

    public func currentCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink? {
        do {
            let decoded: CollectionShareLinkBody = try await request(
                method: "GET", path: "collection-rooms/\(collectionRoomID)/share-link", accessToken: accessToken, body: nil
            )
            return try decoded.model()
        } catch let error as CollectionAPIError where error.statusCode == 404 {
            return nil
        }
    }

    public func regenerateCollectionShareLink(accessToken: String, collectionRoomID: String) async throws -> CollectionRoomShareLink {
        let decoded: CollectionShareLinkBody = try await request(
            method: "POST", path: "collection-rooms/\(collectionRoomID)/share-link/regenerate", accessToken: accessToken, body: nil
        )
        return try decoded.model()
    }

    public func revokeCollectionShareLink(accessToken: String, collectionRoomID: String) async throws {
        try await requestExpectingNoContent(
            method: "DELETE", path: "collection-rooms/\(collectionRoomID)/share-link", accessToken: accessToken
        )
    }

    public func sharedCollectionRoom(accessToken: String, code: String) async throws -> SharedCollectionRoomContent {
        let decoded: SharedCollectionRoomBody = try await request(
            method: "GET", path: "collection-share-links/\(code)/collection-room", accessToken: accessToken, body: nil
        )
        return decoded.model
    }

    // MARK: - Transport

    private func url(path: String, query: [String: String]) -> URL {
        let base = baseURL.appendingPathComponent(path)
        guard !query.isEmpty,
              var components = URLComponents(url: base, resolvingAgainstBaseURL: false)
        else { return base }
        components.queryItems = query.sorted { $0.key < $1.key }
            .map { URLQueryItem(name: $0.key, value: $0.value) }
        return components.url ?? base
    }

    private func send(
        method: String,
        path: String,
        query: [String: String],
        accessToken: String,
        body: Data?
    ) async throws -> (Data, HTTPURLResponse) {
        var request = URLRequest(url: url(path: path, query: query))
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
            let errorBody = try? JSONDecoder().decode(CollectionErrorBody.self, from: data)
            throw CollectionAPIError(
                statusCode: httpResponse.statusCode,
                code: errorBody?.code,
                message: errorBody?.error
            )
        }
        return (data, httpResponse)
    }

    private func request<Response: Decodable>(
        method: String,
        path: String,
        query: [String: String] = [:],
        accessToken: String,
        body: Data?
    ) async throws -> Response {
        let (data, _) = try await send(
            method: method, path: path, query: query, accessToken: accessToken, body: body
        )
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .rfc3339
        return try decoder.decode(Response.self, from: data)
    }

    private func requestExpectingNoContent(
        method: String,
        path: String,
        accessToken: String
    ) async throws {
        _ = try await send(method: method, path: path, query: [:], accessToken: accessToken, body: nil)
    }
}

public struct CollectionAPIError: Error, Equatable, Sendable {
    public let statusCode: Int
    public let code: String?
    public let message: String?

    public init(statusCode: Int, code: String?, message: String?) {
        self.statusCode = statusCode
        self.code = code
        self.message = message
    }

    public var isUnknownCategory: Bool { code == "unknown_category" }
    public var isCategoryRequired: Bool { code == "category_required" }
    public var isDesignNotApplicable: Bool { code == "design_not_applicable" }
    public var isModelNotAvailable: Bool { code == "model_not_available" }
    public var isItemNotInRoom: Bool { code == "item_not_in_room" }
    public var isItemCapacityReached: Bool { code == "item_capacity_reached" }
    public var isSlotTaken: Bool { code == "slot_taken" }
    public var isSlotNotAvailable: Bool { code == "slot_not_available" }
    public var isNameProblem: Bool {
        code == "name_required" || code == "name_too_long" || code == "invalid_name"
    }
}

// MARK: - Wire types

private struct CreateCollectionRoomBody: Encodable {
    let name: String
    let categoryID: String?
    let designID: String?
    enum CodingKeys: String, CodingKey {
        case name
        case categoryID = "category_id"
        case designID = "design_id"
    }
}

private struct UpdateCollectionRoomBody: Encodable {
    let name: String?
    let categoryID: String?
    let designID: String?

    enum CodingKeys: String, CodingKey {
        case name
        case categoryID = "category_id"
        case designID = "design_id"
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(name, forKey: .name)
        try container.encodeIfPresent(categoryID, forKey: .categoryID)
        try container.encodeIfPresent(designID, forKey: .designID)
    }
}

private struct CollectionRoomBody: Decodable {
    let id: String
    let name: String
    let categoryID: String
    let designID: String
    let currentTier: Int
    let musicTrackID: String?
    let items: [CollectionItemBody]

    enum CodingKeys: String, CodingKey {
        case id, name, items
        case categoryID = "category_id"
        case designID = "design_id"
        case currentTier = "current_tier"
        case musicTrackID = "music_track_id"
    }

    var model: CollectionRoom {
        CollectionRoom(
            id: id,
            name: name,
            categoryID: categoryID.isEmpty ? nil : categoryID,
            designID: designID.isEmpty ? nil : designID,
            currentTier: CollectionTier(currentTier),
            items: items.map(\.model),
            musicTrackID: musicTrackID
        )
    }
}

private struct AssignCollectionMusicRequestBody: Encodable {
    let musicTrackID: String
    enum CodingKeys: String, CodingKey {
        case musicTrackID = "music_track_id"
    }
}

private struct CollectionItemBody: Decodable {
    let id: String
    let slotIndex: Int
    let catalogModelID: String

    enum CodingKeys: String, CodingKey {
        case id
        case slotIndex = "slot_index"
        case catalogModelID = "catalog_model_id"
    }

    var model: CollectionItem {
        CollectionItem(id: id, slotIndex: slotIndex, catalogModelID: catalogModelID)
    }
}

private struct CollectionRoomListBody: Decodable {
    let collectionRooms: [CollectionRoomBody]
    enum CodingKeys: String, CodingKey { case collectionRooms = "collection_rooms" }
}

private struct CollectionCategoryBody: Decodable {
    let id: String
    let displayName: String
    let sortOrder: Int

    enum CodingKeys: String, CodingKey {
        case id
        case displayName = "display_name"
        case sortOrder = "sort_order"
    }

    var model: CollectionCategory {
        CollectionCategory(id: id, displayName: displayName, sortOrder: sortOrder)
    }
}

private struct CollectionCategoryListBody: Decodable {
    let collectionCategories: [CollectionCategoryBody]
    enum CodingKeys: String, CodingKey { case collectionCategories = "collection_categories" }
}

private struct CollectionModelBody: Decodable {
    let id: String
    let brandID: String
    let brandDisplayName: String
    let categoryID: String
    let displayName: String
    let metadata: JSONValue?
    let hasAsset: Bool
    let assetBundleID: String?
    let assetBundleVersion: Int?
    let isDevelopmentFixture: Bool

    enum CodingKeys: String, CodingKey {
        case id
        case brandID = "brand_id"
        case brandDisplayName = "brand_display_name"
        case categoryID = "category_id"
        case displayName = "display_name"
        case metadata
        case hasAsset = "has_asset"
        case assetBundleID = "asset_bundle_id"
        case assetBundleVersion = "asset_bundle_version"
        case isDevelopmentFixture = "is_development_fixture"
    }

    var model: CollectionCatalogModel {
        var bundle: AssetBundleRef?
        if hasAsset, let id = assetBundleID, !id.isEmpty {
            bundle = AssetBundleRef(id: id, version: assetBundleVersion ?? 1)
        }
        return CollectionCatalogModel(
            id: id,
            brandID: brandID,
            brandDisplayName: brandDisplayName,
            categoryID: categoryID,
            displayName: displayName,
            metadata: metadata?.rawData ?? Data("{}".utf8),
            hasAsset: hasAsset,
            assetBundle: bundle,
            isDevelopmentFixture: isDevelopmentFixture
        )
    }
}

private indirect enum JSONValue: Codable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
        } else {
            self = .object(try container.decode([String: JSONValue].self))
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .null: try container.encodeNil()
        case .bool(let value): try container.encode(value)
        case .number(let value): try container.encode(value)
        case .string(let value): try container.encode(value)
        case .array(let value): try container.encode(value)
        case .object(let value): try container.encode(value)
        }
    }

    var rawData: Data {
        (try? JSONEncoder().encode(self)) ?? Data("{}".utf8)
    }
}

private struct ModelSearchPageBody: Decodable {
    let models: [CollectionModelBody]
    let nextCursorName: String?
    let nextCursorID: String?

    enum CodingKeys: String, CodingKey {
        case models
        case nextCursorName = "next_cursor_name"
        case nextCursorID = "next_cursor_id"
    }

    var model: CollectionModelSearchPage {
        var cursor: CollectionModelSearchCursor?
        if let name = nextCursorName, let id = nextCursorID, !name.isEmpty, !id.isEmpty {
            cursor = CollectionModelSearchCursor(displayName: name, id: id)
        }
        return CollectionModelSearchPage(models: models.map(\.model), nextCursor: cursor)
    }
}

private struct PresentationAssetBody: Decodable {
    let modelID: String
    let hasPresentationAsset: Bool
    let assetBundleID: String?
    let assetBundleVersion: Int?
    let isDevelopmentFixture: Bool

    enum CodingKeys: String, CodingKey {
        case modelID = "model_id"
        case hasPresentationAsset = "has_presentation_asset"
        case assetBundleID = "asset_bundle_id"
        case assetBundleVersion = "asset_bundle_version"
        case isDevelopmentFixture = "is_development_fixture"
    }

    var entry: CollectionPresentationAssetEntry {
        guard hasPresentationAsset, let bundleID = assetBundleID, !bundleID.isEmpty else {
            return CollectionPresentationAssetEntry(catalogModelID: modelID, asset: nil)
        }
        return CollectionPresentationAssetEntry(
            catalogModelID: modelID,
            asset: CollectionItemPresentationAsset(
                catalogModelID: modelID,
                assetBundle: AssetBundleRef(id: bundleID, version: assetBundleVersion ?? 1),
                isDevelopmentFixture: isDevelopmentFixture
            )
        )
    }
}

private struct PresentationAssetsBody: Decodable {
    let assets: [PresentationAssetBody]
}

private struct RatchetTierBody: Encodable {
    let tier: Int
}

private struct AddCollectionItemBody: Encodable {
    let catalogModelID: String
    enum CodingKeys: String, CodingKey { case catalogModelID = "catalog_model_id" }
}

private struct PlaceCollectionItemBody: Encodable {
    let slotIndex: Int
    enum CodingKeys: String, CodingKey { case slotIndex = "slot_index" }
}

private struct CollectionDesignBody: Decodable {
    let id: String
    let displayName: String
    let categoryID: String?
    let isDevelopmentFixture: Bool
    let assetBundleID: String
    let assetBundleVersion: Int
    let sortOrder: Int
    let tierCount: Int?

    enum CodingKeys: String, CodingKey {
        case id
        case displayName = "display_name"
        case categoryID = "category_id"
        case isDevelopmentFixture = "is_development_fixture"
        case assetBundleID = "asset_bundle_id"
        case assetBundleVersion = "asset_bundle_version"
        case sortOrder = "sort_order"
        case tierCount = "tier_count"
    }

    var model: CollectionDesign {
        CollectionDesign(
            id: id,
            displayName: displayName,
            categoryID: categoryID,
            isDevelopmentFixture: isDevelopmentFixture,
            assetBundle: AssetBundleRef(id: assetBundleID, version: assetBundleVersion),
            sortOrder: sortOrder,
            tierCount: tierCount ?? 1
        )
    }
}

private struct CollectionDesignListBody: Decodable {
    let collectionDesigns: [CollectionDesignBody]
    enum CodingKeys: String, CodingKey { case collectionDesigns = "collection_designs" }
}

private struct CollectionErrorBody: Decodable {
    let error: String?
    let code: String?
}

// MARK: - Sharing wire types

private struct CollectionShareLinkBody: Decodable {
    let collectionRoomID: String
    let code: String
    let url: String
    let createdAt: Date

    enum CodingKeys: String, CodingKey {
        case code, url
        case collectionRoomID = "collection_room_id"
        case createdAt = "created_at"
    }

    func model() throws -> CollectionRoomShareLink {
        guard let parsedURL = URL(string: url) else {
            throw IdentityAPIClientError.invalidResponse
        }
        return CollectionRoomShareLink(collectionRoomID: collectionRoomID, code: code, url: parsedURL, createdAt: createdAt)
    }
}

private struct SharedCollectionRoomBody: Decodable {
    let collectionRoomID: String
    let name: String
    let categoryID: String
    let designID: String
    let currentTier: Int
    let musicTrackID: String?
    let items: [CollectionItemBody]

    enum CodingKeys: String, CodingKey {
        case name, items
        case collectionRoomID = "collection_room_id"
        case categoryID = "category_id"
        case designID = "design_id"
        case currentTier = "current_tier"
        case musicTrackID = "music_track_id"
    }

    var model: SharedCollectionRoomContent {
        SharedCollectionRoomContent(
            collectionRoomID: collectionRoomID,
            name: name,
            categoryID: categoryID.isEmpty ? nil : categoryID,
            designID: designID.isEmpty ? nil : designID,
            currentTier: CollectionTier(currentTier),
            items: items.map(\.model),
            musicTrackID: musicTrackID
        )
    }
}
