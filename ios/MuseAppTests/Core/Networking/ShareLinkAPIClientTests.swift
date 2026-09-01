import XCTest
@testable import MuseApp

final class ShareLinkAPIClientTests: XCTestCase {
    override func tearDown() {
        ShareStubURLProtocol.stub = nil
        ShareStubURLProtocol.lastRequest = nil
        super.tearDown()
    }

    private func makeClient() -> ShareLinkAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ShareStubURLProtocol.self]
        return ShareLinkAPIClient(baseURL: URL(string: "https://example.invalid")!, session: URLSession(configuration: configuration))
    }

    private func stub(_ json: String, statusCode: Int = 200) {
        ShareStubURLProtocol.stub = .init(statusCode: statusCode, body: Data(json.utf8))
    }

    private let linkJSON = #"{"code":"abcdefghijklmnopqrstuv","url":"https://muse.app/m/abcdefghijklmnopqrstuv","created_at":"2026-08-25T12:00:00Z"}"#

    func test_ensureShareLink_postsToTheOwnersRoute_withTheToken_andNoBody() async throws {
        stub(linkJSON)

        let link = try await makeClient().ensureShareLink(accessToken: "token")

        XCTAssertEqual(link.code, "abcdefghijklmnopqrstuv")
        XCTAssertEqual(link.url.absoluteString, "https://muse.app/m/abcdefghijklmnopqrstuv")
        let request = try XCTUnwrap(ShareStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/museum/me/share-link")
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token")
        XCTAssertNil(request.httpBody, "the owner's Museum is implied by the token — no id is sent")
    }

    func test_currentShareLink_404_isNil_notAnError() async throws {
        stub(#"{"error":"not found"}"#, statusCode: 404)

        let link = try await makeClient().currentShareLink(accessToken: "token")

        XCTAssertNil(link)
        XCTAssertEqual(ShareStubURLProtocol.lastRequest?.httpMethod, "GET")
    }

    func test_regenerateShareLink_postsToTheRegenerateRoute() async throws {
        stub(linkJSON)

        _ = try await makeClient().regenerateShareLink(accessToken: "token")

        XCTAssertEqual(ShareStubURLProtocol.lastRequest?.url?.path, "/museum/me/share-link/regenerate")
        XCTAssertEqual(ShareStubURLProtocol.lastRequest?.httpMethod, "POST")
    }

    func test_preview_sendsNoToken_andDecodesOnlyTheSafeFields() async throws {
        stub(#"{"code":"abcdefghijklmnopqrstuv","style_id":"style_modern","owner":{"avatar_id":"avatar_2"}}"#)

        let preview = try await makeClient().preview(code: "abcdefghijklmnopqrstuv")

        XCTAssertEqual(preview, ShareLinkPreview(code: "abcdefghijklmnopqrstuv", styleID: "style_modern", ownerAvatarID: "avatar_2"))
        let request = try XCTUnwrap(ShareStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/share-links/abcdefghijklmnopqrstuv")
        XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"), "the preview is pre-authentication")
    }

    func test_sharedMuseum_isAddressedByCode_withTheToken() async throws {
        stub(#"{"museum_id":"m1","style_id":"style_modern","rooms":[{"id":"r1","name":"Hall","variant_id":"v1"}]}"#)

        let content = try await makeClient().sharedMuseum(accessToken: "token", code: "abcdefghijklmnopqrstuv")

        XCTAssertEqual(content, SharedMuseumContent(museumID: "m1", styleID: "style_modern", rooms: [SharedRoomSummary(id: "r1", name: "Hall", variantID: "v1")]))
        let request = try XCTUnwrap(ShareStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/share-links/abcdefghijklmnopqrstuv/museum")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token")
    }

    // MARK: - Timestamps ( close-out: the live defect)

    func test_museumLink_decodesCreatedAt_withAndWithoutFractionalSeconds() async throws {
        let whole = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:24:58Z"))
        let cases: [(json: String, offset: TimeInterval)] = [
            ("2026-08-26T22:24:58Z", 0),
            ("2026-08-26T22:24:58.454Z", 0.454),
            ("2026-08-26T22:24:58.454662Z", 0.454662),
            ("2026-08-27T01:24:58.454662+03:00", 0.454662),
        ]
        for testCase in cases {
            stub(#"{"code":"abcdefghijklmnopqrstuv","url":"https://muse.app/m/abcdefghijklmnopqrstuv","created_at":"\#(testCase.json)"}"#)

            let link = try await makeClient().ensureShareLink(accessToken: "token")

            XCTAssertEqual(link.createdAt.timeIntervalSince(whole), testCase.offset, accuracy: 1e-6, "created_at \(testCase.json)")
            XCTAssertEqual(link.code, "abcdefghijklmnopqrstuv")
        }
    }

    func test_photoTickets_decodeExpiresAt_withFractionalSeconds() async throws {
        stub(#"{"tickets":[{"photo_asset_id":"a1","url":"https://cdn.example/a1?sig=x","expires_at":"2026-08-26T22:29:58.454662Z","pixel_width":1024,"pixel_height":768}]}"#)

        let tickets = try await makeClient().sharedRoomPhotoURLs(accessToken: "token", code: "abcdefghijklmnopqrstuv", roomID: "r1")

        XCTAssertEqual(tickets.count, 1)
        let expected = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:29:58.454662Z"))
        XCTAssertEqual(tickets[0].expiresAt.timeIntervalSince1970, expected.timeIntervalSince1970, accuracy: 1e-6)
        XCTAssertEqual(tickets[0].photoAssetID, "a1")
    }

    func test_malformedCreatedAt_failsTheDecode_ratherThanBecomingAWrongValue() async {
        for malformed in ["not-a-date", "2026-08-26 22:24:58Z", "2026-08-26T22:24:58.Z", "2026-13-40T22:24:58Z", ""] {
            stub(#"{"code":"abcdefghijklmnopqrstuv","url":"https://muse.app/m/abcdefghijklmnopqrstuv","created_at":"\#(malformed)"}"#)
            do {
                _ = try await makeClient().ensureShareLink(accessToken: "token")
                XCTFail("expected \(malformed.debugDescription) to fail the decode")
            } catch is DecodingError {
            } catch {
                XCTFail("expected a DecodingError for \(malformed.debugDescription), got \(error)")
            }
        }
    }

    func test_refusals_surfaceAsTheCollapsedNotFound() async {
        stub(#"{"error":"not found"}"#, statusCode: 404)

        do {
            _ = try await makeClient().preview(code: "abcdefghijklmnopqrstuv")
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, let message) {
            XCTAssertEqual(statusCode, 404)
            XCTAssertEqual(message, "not found")
        } catch {
            XCTFail("expected .server, got \(error)")
        }
    }
}

private final class ShareStubURLProtocol: URLProtocol {
    struct Stub {
        let statusCode: Int
        let body: Data
    }

    nonisolated(unsafe) static var stub: Stub?
    nonisolated(unsafe) static var lastRequest: URLRequest?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lastRequest = request
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
