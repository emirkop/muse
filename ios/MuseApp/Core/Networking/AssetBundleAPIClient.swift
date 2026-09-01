import Foundation

public final class AssetBundleAPIClient: AssetBundleManifestFetching, RoomVariantCatalogLookup, Sendable {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = NetworkResilience.apiSession()) {
        self.baseURL = baseURL
        self.session = session
    }

    public func manifest(accessToken: String, bundleID: String, appAssetVersion: Int) async throws -> AssetBundleManifest {
        var components = URLComponents(
            url: baseURL.appendingPathComponent("catalog/bundles/\(bundleID)/manifest"),
            resolvingAgainstBaseURL: false
        )
        components?.queryItems = [URLQueryItem(name: "app_asset_version", value: String(appAssetVersion))]
        guard let url = components?.url else { throw IdentityAPIClientError.invalidResponse }

        let decoded: ManifestResponseBody = try await request(url: url, accessToken: accessToken)
        return try decoded.model()
    }

    public func variant(accessToken: String, variantID: String) async throws -> RoomVariant? {
        let url = baseURL.appendingPathComponent("catalog/room-variants/\(variantID)")
        do {
            let decoded: VariantResponseBody = try await request(url: url, accessToken: accessToken)
            return decoded.model
        } catch let error as PhotoAPIError where error.statusCode == 404 {
            return nil
        }
    }

    // MARK: - Transport

    private func request<Response: Decodable>(url: URL, accessToken: String) async throws -> Response {
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.resilientData(for: request)
        } catch {
            throw IdentityAPIClientError.classify(error)
        }
        guard let http = response as? HTTPURLResponse else { throw IdentityAPIClientError.invalidResponse }
        guard (200...299).contains(http.statusCode) else {
            let errorBody = try? JSONDecoder().decode(BundleErrorResponseBody.self, from: data)
            throw PhotoAPIError(statusCode: http.statusCode, message: errorBody?.error, code: nil, assetID: nil)
        }
        return try JSONDecoder().decode(Response.self, from: data)
    }
}

// MARK: - Wire shapes

private struct BundleErrorResponseBody: Decodable {
    let error: String?
}

private struct ManifestResponseBody: Decodable {
    let bundleID: String
    let version: Int
    let kind: String
    let format: String
    let minAppVersion: Int
    let files: [FileBody]
    let dependencies: [DependencyBody]

    struct FileBody: Decodable {
        let assetID: String
        let role: String
        let url: String
        let contentType: String
        let byteSize: Int64
        let checksumSHA256: String

        enum CodingKeys: String, CodingKey {
            case assetID = "asset_id"
            case role
            case url
            case contentType = "content_type"
            case byteSize = "byte_size"
            case checksumSHA256 = "checksum_sha256"
        }
    }

    struct DependencyBody: Decodable {
        let bundleID: String
        let version: Int

        enum CodingKeys: String, CodingKey {
            case bundleID = "bundle_id"
            case version
        }
    }

    enum CodingKeys: String, CodingKey {
        case bundleID = "bundle_id"
        case version
        case kind
        case format
        case minAppVersion = "min_app_version"
        case files
        case dependencies
    }

    func model() throws -> AssetBundleManifest {
        guard let kind = AssetBundleKind(rawValue: kind) else {
            throw IdentityAPIClientError.invalidResponse
        }
        var files: [AssetBundleFile] = []
        for file in self.files {
            guard let role = AssetRole(rawValue: file.role) else {
                continue
            }
            guard let url = URL(string: file.url), file.byteSize > 0, file.checksumSHA256.count == 64 else {
                throw IdentityAPIClientError.invalidResponse
            }
            files.append(AssetBundleFile(
                assetID: file.assetID,
                role: role,
                url: url,
                contentType: file.contentType,
                byteSize: file.byteSize,
                checksumSHA256: file.checksumSHA256.lowercased()
            ))
        }
        guard files.contains(where: { $0.role == .geometry }) else {
            throw IdentityAPIClientError.invalidResponse
        }
        return AssetBundleManifest(
            identity: AssetBundleIdentity(bundleID: bundleID, version: version),
            kind: kind,
            format: format,
            minAppVersion: minAppVersion,
            files: files,
            dependencies: dependencies.map { AssetBundleIdentity(bundleID: $0.bundleID, version: $0.version) }
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
