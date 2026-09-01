import Foundation

public final class EntitlementAPIClient: EntitlementServicing, @unchecked Sendable {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = NetworkResilience.apiSession()) {
        self.baseURL = baseURL
        self.session = session
    }

    public func fetchEntitlement(accessToken: String) async throws -> AccountEntitlement {
        let decoded: StatusBody = try await request(method: "GET", path: "entitlements/me", accessToken: accessToken, body: nil)
        return decoded.model
    }

    public func appAccountToken(accessToken: String) async throws -> UUID {
        let decoded: TokenBody = try await request(method: "POST", path: "entitlements/app-account-token", accessToken: accessToken, body: nil)
        guard let uuid = UUID(uuidString: decoded.appAccountToken) else {
            throw IdentityAPIClientError.invalidResponse
        }
        return uuid
    }

    public func redeem(accessToken: String, signedTransaction: String) async throws -> AccountEntitlement {
        let body = try JSONEncoder().encode(RedeemBody(signedTransaction: signedTransaction))
        let decoded: StatusBody = try await request(
            method: "POST", path: "entitlements/app-store/transactions", accessToken: accessToken, body: body
        )
        return decoded.model
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
            let errorBody = try? JSONDecoder().decode(ErrorBody.self, from: data)
            throw EntitlementAPIError(statusCode: http.statusCode, code: errorBody?.code, message: errorBody?.error)
        }
        return try JSONDecoder().decode(Response.self, from: data)
    }
}

public struct EntitlementAPIError: Error, Equatable, Sendable {
    public let statusCode: Int
    public let code: String?
    public let message: String?

    public init(statusCode: Int, code: String?, message: String?) {
        self.statusCode = statusCode
        self.code = code
        self.message = message
    }

    public var isBoundToAnotherAccount: Bool {
        code == "app_account_token_mismatch" || code == "transaction_bound_to_another_account"
    }
    public var isNotApplicable: Bool {
        code == "invalid_signed_transaction" || code == "transaction_not_applicable"
    }
    public var isVerificationUnavailable: Bool { code == "verification_unavailable" }
}

// MARK: - Wire types

private struct StatusBody: Decodable {
    let state: String
    let itemCapacity: Int
    let itemCount: Int

    enum CodingKeys: String, CodingKey {
        case state
        case itemCapacity = "item_capacity"
        case itemCount = "item_count"
    }

    var model: AccountEntitlement {
        AccountEntitlement(
            state: EntitlementState(rawValue: state) ?? .unknown,
            itemCapacity: itemCapacity,
            itemCount: itemCount
        )
    }
}

private struct TokenBody: Decodable {
    let appAccountToken: String
    enum CodingKeys: String, CodingKey {
        case appAccountToken = "app_account_token"
    }
}

private struct RedeemBody: Encodable {
    let signedTransaction: String
    enum CodingKeys: String, CodingKey {
        case signedTransaction = "signed_transaction"
    }
}

private struct ErrorBody: Decodable {
    let error: String
    let code: String?
}
