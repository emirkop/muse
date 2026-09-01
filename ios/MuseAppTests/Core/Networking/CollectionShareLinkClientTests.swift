import XCTest
@testable import MuseApp

final class CollectionShareLinkClientTests: XCTestCase {
    override func tearDown() {
        CollectionShareStubURLProtocol.stub = nil
        CollectionShareStubURLProtocol.lastRequest = nil
        super.tearDown()
    }

    private func makeClient() -> CollectionAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CollectionShareStubURLProtocol.self]
        return CollectionAPIClient(baseURL: URL(string: "https://example.invalid")!, session: URLSession(configuration: configuration))
    }

    private func stub(_ json: String, statusCode: Int = 200) {
        CollectionShareStubURLProtocol.stub = .init(statusCode: statusCode, body: Data(json.utf8))
    }

    private let code = "abcdefghijklmnopqrstuv"

    private func linkJSON(createdAt: String) -> String {
        #"{"collection_room_id":"cr-1","code":"\#(code)","url":"https://muse.app/c/\#(code)","created_at":"\#(createdAt)"}"#
    }

    // MARK: - Owner

    func test_ensure_postsToTheRoomsRoute_andDecodesAGoTimestamp() async throws {
        stub(linkJSON(createdAt: "2026-08-26T22:24:58.454662Z"))

        let link = try await makeClient().ensureCollectionShareLink(accessToken: "token", collectionRoomID: "cr-1")

        XCTAssertEqual(link.collectionRoomID, "cr-1")
        XCTAssertEqual(link.code, code)
        XCTAssertEqual(link.url.absoluteString, "https://muse.app/c/\(code)")
        let expected = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:24:58.454662Z"))
        XCTAssertEqual(link.createdAt.timeIntervalSince1970, expected.timeIntervalSince1970, accuracy: 1e-6)
        let request = try XCTUnwrap(CollectionShareStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/collection-rooms/cr-1/share-link")
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token")
        XCTAssertNil(request.httpBody)
    }

    func test_link_decodesCreatedAt_inEveryShapeTheServerProduces() async throws {
        let whole = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:24:58Z"))
        for (text, offset) in [("2026-08-26T22:24:58Z", 0.0), ("2026-08-26T22:24:58.454Z", 0.454), ("2026-08-26T22:24:58.454662Z", 0.454662)] {
            stub(linkJSON(createdAt: text))

            let link = try await makeClient().regenerateCollectionShareLink(accessToken: "token", collectionRoomID: "cr-1")

            XCTAssertEqual(link.createdAt.timeIntervalSince(whole), offset, accuracy: 1e-6, text)
            XCTAssertEqual(CollectionShareStubURLProtocol.lastRequest?.url?.path, "/collection-rooms/cr-1/share-link/regenerate")
        }
    }

    func test_malformedCreatedAt_failsTheDecode() async {
        stub(linkJSON(createdAt: "2026-08-26 22:24:58"))
        do {
            _ = try await makeClient().ensureCollectionShareLink(accessToken: "token", collectionRoomID: "cr-1")
            XCTFail("expected the decode to fail")
        } catch is DecodingError {
        } catch {
            XCTFail("expected a DecodingError, got \(error)")
        }
    }

    func test_current_404_isNil_notAnError() async throws {
        stub(#"{"error":"not found"}"#, statusCode: 404)

        let link = try await makeClient().currentCollectionShareLink(accessToken: "token", collectionRoomID: "cr-1")

        XCTAssertNil(link)
        XCTAssertEqual(CollectionShareStubURLProtocol.lastRequest?.httpMethod, "GET")
        XCTAssertEqual(CollectionShareStubURLProtocol.lastRequest?.url?.path, "/collection-rooms/cr-1/share-link")
    }

    func test_revoke_deletesTheLink_andAcceptsNoContent() async throws {
        stub("", statusCode: 204)

        try await makeClient().revokeCollectionShareLink(accessToken: "token", collectionRoomID: "cr-1")

        let request = try XCTUnwrap(CollectionShareStubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/collection-rooms/cr-1/share-link")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token")
    }

    // MARK: - Visitor

    func test_sharedCollectionRoom_isAddressedByCode_withTheToken_andDecodesReferencesOnly() async throws {
        stub(#"{"collection_room_id":"cr-1","name":"Shared Watches","category_id":"category_watches","design_id":"","current_tier":2,"items":[{"id":"i1","slot_index":3,"catalog_model_id":"model-1"}]}"#)

        let content = try await makeClient().sharedCollectionRoom(accessToken: "token", code: code)

        XCTAssertEqual(content, SharedCollectionRoomContent(
            collectionRoomID: "cr-1",
            name: "Shared Watches",
            categoryID: "category_watches",
            designID: nil,
            currentTier: CollectionTier(2),
            items: [CollectionItem(id: "i1", slotIndex: 3, catalogModelID: "model-1")]
        ))
        let request = try XCTUnwrap(CollectionShareStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/collection-share-links/\(code)/collection-room")
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token", "the visitor read is authenticated")
    }

    func test_refusal_surfacesAsTheCollapsedNotFound() async {
        stub(#"{"error":"not found"}"#, statusCode: 404)
        do {
            _ = try await makeClient().sharedCollectionRoom(accessToken: "token", code: code)
            XCTFail("expected an error")
        } catch let error as CollectionAPIError {
            XCTAssertEqual(error.statusCode, 404)
        } catch {
            XCTFail("expected CollectionAPIError, got \(error)")
        }
    }
}

private final class CollectionShareStubURLProtocol: URLProtocol {
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
