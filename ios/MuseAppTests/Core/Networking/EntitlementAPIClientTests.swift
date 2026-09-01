import XCTest
@testable import MuseApp

final class EntitlementAPIClientTests: XCTestCase {
    override func tearDown() {
        EntitlementStubURLProtocol.stub = nil
        EntitlementStubURLProtocol.lastRequest = nil
        EntitlementStubURLProtocol.lastBody = nil
        super.tearDown()
    }

    private func makeClient() -> EntitlementAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [EntitlementStubURLProtocol.self]
        return EntitlementAPIClient(baseURL: URL(string: "https://example.invalid")!, session: URLSession(configuration: configuration))
    }

    private func stub(_ json: String, statusCode: Int = 200) {
        EntitlementStubURLProtocol.stub = .init(statusCode: statusCode, body: Data(json.utf8))
    }

    func test_fetchEntitlement_decodesTheServersState_andNumbers() async throws {
        stub(#"{"state":"revoked","item_capacity":3,"item_count":5}"#)

        let entitlement = try await makeClient().fetchEntitlement(accessToken: "token")

        XCTAssertEqual(entitlement, AccountEntitlement(state: .revoked, itemCapacity: 3, itemCount: 5))
        XCTAssertTrue(entitlement.isAtCapacity)
        let request = try XCTUnwrap(EntitlementStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/entitlements/me")
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token")
    }

    func test_unknownState_isNotPaid() async throws {
        stub(#"{"state":"platinum","item_capacity":99,"item_count":0}"#)
        let entitlement = try await makeClient().fetchEntitlement(accessToken: "token")
        XCTAssertEqual(entitlement.state, .unknown)
        XCTAssertTrue(entitlement.canUpgrade)
    }

    func test_appAccountToken_postsAndDecodesAUUID() async throws {
        stub(#"{"app_account_token":"6f9619ff-8b86-d011-b42d-00c04fc964ff"}"#)

        let token = try await makeClient().appAccountToken(accessToken: "token")

        XCTAssertEqual(token, UUID(uuidString: "6f9619ff-8b86-d011-b42d-00c04fc964ff"))
        XCTAssertEqual(EntitlementStubURLProtocol.lastRequest?.url?.path, "/entitlements/app-account-token")
        XCTAssertEqual(EntitlementStubURLProtocol.lastRequest?.httpMethod, "POST")
    }

    func test_redeem_sendsTheSignedTransaction_andAdoptsTheServersStatus() async throws {
        stub(#"{"state":"paid","item_capacity":6,"item_count":3}"#)

        let entitlement = try await makeClient().redeem(accessToken: "token", signedTransaction: "eyJ.eyJ.sig")

        XCTAssertEqual(entitlement.state, .paid)
        XCTAssertEqual(entitlement.itemCapacity, 6)
        let request = try XCTUnwrap(EntitlementStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/entitlements/app-store/transactions")
        XCTAssertEqual(request.httpMethod, "POST")
        let body = try JSONDecoder().decode([String: String].self, from: try XCTUnwrap(EntitlementStubURLProtocol.lastBody))
        XCTAssertEqual(body, ["signed_transaction": "eyJ.eyJ.sig"], "signed bytes only — there is no field for a client to claim a state")
    }

    func test_refusals_surfaceWithTheirStableCodes() async {
        for (json, status, check) in [
            (#"{"error":"another account","code":"app_account_token_mismatch"}"#, 409, { (e: EntitlementAPIError) in e.isBoundToAnotherAccount }),
            (#"{"error":"bound","code":"transaction_bound_to_another_account"}"#, 409, { (e: EntitlementAPIError) in e.isBoundToAnotherAccount }),
            (#"{"error":"nope","code":"invalid_signed_transaction"}"#, 400, { (e: EntitlementAPIError) in e.isNotApplicable }),
            (#"{"error":"nope","code":"transaction_not_applicable"}"#, 400, { (e: EntitlementAPIError) in e.isNotApplicable }),
            (#"{"error":"later","code":"verification_unavailable"}"#, 503, { (e: EntitlementAPIError) in e.isVerificationUnavailable }),
        ] {
            stub(json, statusCode: status)
            do {
                _ = try await makeClient().redeem(accessToken: "token", signedTransaction: "x.y.z")
                XCTFail("expected a refusal for \(json)")
            } catch let error as EntitlementAPIError {
                XCTAssertEqual(error.statusCode, status)
                XCTAssertTrue(check(error), json)
            } catch {
                XCTFail("expected EntitlementAPIError, got \(error)")
            }
        }
    }
}

private final class EntitlementStubURLProtocol: URLProtocol {
    struct Stub {
        let statusCode: Int
        let body: Data
    }

    nonisolated(unsafe) static var stub: Stub?
    nonisolated(unsafe) static var lastRequest: URLRequest?
    nonisolated(unsafe) static var lastBody: Data?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lastRequest = request
        if let stream = request.httpBodyStream {
            stream.open()
            var data = Data()
            let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: 4096)
            defer { buffer.deallocate() }
            while stream.hasBytesAvailable {
                let read = stream.read(buffer, maxLength: 4096)
                if read <= 0 { break }
                data.append(buffer, count: read)
            }
            stream.close()
            Self.lastBody = data
        } else {
            Self.lastBody = request.httpBody
        }
        guard let stub = Self.stub else {
            client?.urlProtocolDidFinishLoading(self)
            return
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: stub.statusCode, httpVersion: "HTTP/1.1", headerFields: nil)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: stub.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
