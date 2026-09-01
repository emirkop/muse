import XCTest
@testable import MuseApp

final class CollectionCatalogSearchClientTests: XCTestCase {
    override func tearDown() {
        CatalogSearchStubURLProtocol.stub = nil
        CatalogSearchStubURLProtocol.lastRequest = nil
        super.tearDown()
    }

    private func makeClient() -> CollectionAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CatalogSearchStubURLProtocol.self]
        return CollectionAPIClient(
            baseURL: URL(string: "https://example.invalid")!,
            session: URLSession(configuration: configuration)
        )
    }

    private func stub(_ json: String, statusCode: Int = 200) {
        CatalogSearchStubURLProtocol.stub = .init(statusCode: statusCode, body: Data(json.utf8))
    }

    private let liveBody = """
        {
          "models": [
            {
              "id": "dev-fixture:model-chrono-one",
              "brand_id": "dev-fixture:brand-a",
              "brand_display_name": "Devco (development fixture)",
              "category_id": "category_watches",
              "display_name": "Devco Chrono One (development fixture)",
              "metadata": {
                "_comment": "development fixture metadata - not a real product specification"
              },
              "has_asset": true,
              "asset_bundle_id": "dev_fixture_collection_model",
              "asset_bundle_version": 1,
              "is_development_fixture": true
            },
            {
              "id": "dev-fixture:model-chrono-two",
              "brand_id": "dev-fixture:brand-a",
              "brand_display_name": "Devco (development fixture)",
              "category_id": "category_watches",
              "display_name": "Devco Chrono Two (development fixture)",
              "metadata": {
                "_comment": "development fixture metadata - not a real product specification"
              },
              "has_asset": false,
              "is_development_fixture": true
            }
          ],
          "next_cursor_name": "Devco Chrono Two (development fixture)",
          "next_cursor_id": "dev-fixture:model-chrono-two"
        }
        """

    func test_search_sendsTheCategoryScopeTheQueryAndTheToken() async throws {
        stub(liveBody)

        _ = try await makeClient().searchCollectionModels(
            accessToken: "token",
            categoryID: "category_watches",
            query: "devco chrono",
            limit: 2,
            cursor: nil
        )

        let request = try XCTUnwrap(CatalogSearchStubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.path, "/catalog/collection-models")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token")

        let items = try XCTUnwrap(
            URLComponents(url: try XCTUnwrap(request.url), resolvingAgainstBaseURL: false)?.queryItems
        )
        let parameters = Dictionary(uniqueKeysWithValues: items.map { ($0.name, $0.value) })
        XCTAssertEqual(parameters["category_id"], "category_watches")
        XCTAssertEqual(parameters["q"], "devco chrono")
        XCTAssertEqual(parameters["limit"], "2")
        XCTAssertNil(parameters["cursor_name"], "no cursor was supplied, so none is sent")
        XCTAssertNil(parameters["cursor_id"])
    }

    func test_search_omitsAnEmptyQuery() async throws {
        stub(liveBody)

        _ = try await makeClient().searchCollectionModels(
            accessToken: "token", categoryID: "category_watches", query: "", limit: 0, cursor: nil
        )

        let url = try XCTUnwrap(CatalogSearchStubURLProtocol.lastRequest?.url)
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        XCTAssertFalse(items.contains { $0.name == "q" })
        XCTAssertFalse(items.contains { $0.name == "limit" }, "a non-positive limit lets the server default")
    }

    func test_search_sendsBothHalvesOfTheCursor() async throws {
        stub(liveBody)

        _ = try await makeClient().searchCollectionModels(
            accessToken: "token",
            categoryID: "category_watches",
            query: "",
            limit: 25,
            cursor: CollectionModelSearchCursor(
                displayName: "Devco Chrono Two (development fixture)",
                id: "dev-fixture:model-chrono-two"
            )
        )

        let url = try XCTUnwrap(CatalogSearchStubURLProtocol.lastRequest?.url)
        let items = try XCTUnwrap(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems)
        let parameters = Dictionary(uniqueKeysWithValues: items.map { ($0.name, $0.value) })
        XCTAssertEqual(parameters["cursor_name"], "Devco Chrono Two (development fixture)")
        XCTAssertEqual(parameters["cursor_id"], "dev-fixture:model-chrono-two")
    }

    func test_search_decodesTheLiveResponse() async throws {
        stub(liveBody)

        let page = try await makeClient().searchCollectionModels(
            accessToken: "token", categoryID: "category_watches", query: "", limit: 2, cursor: nil
        )

        XCTAssertEqual(page.models.count, 2)
        let first = page.models[0]
        XCTAssertEqual(first.id, "dev-fixture:model-chrono-one")
        XCTAssertEqual(first.brandID, "dev-fixture:brand-a")
        XCTAssertEqual(first.brandDisplayName, "Devco (development fixture)")
        XCTAssertEqual(first.categoryID, "category_watches")
        XCTAssertEqual(first.displayName, "Devco Chrono One (development fixture)")
        XCTAssertTrue(first.isDevelopmentFixture)
        XCTAssertTrue(first.hasAsset)
        XCTAssertEqual(first.assetBundle?.id, "dev_fixture_collection_model")
        XCTAssertEqual(first.assetBundle?.version, 1)

        let second = page.models[1]
        XCTAssertFalse(second.hasAsset)
        XCTAssertNil(second.assetBundle, "an unauthored asset is an absent reference, not an empty one")

        XCTAssertEqual(page.nextCursor?.id, "dev-fixture:model-chrono-two")
        XCTAssertTrue(page.hasMore)
    }

    func test_search_carriesMetadataWithoutInterpretingIt() async throws {
        stub(liveBody)

        let page = try await makeClient().searchCollectionModels(
            accessToken: "token", categoryID: "category_watches", query: "", limit: 2, cursor: nil
        )

        let decoded = try JSONSerialization.jsonObject(with: page.models[0].metadata) as? [String: Any]
        XCTAssertEqual(
            decoded?["_comment"] as? String,
            "development fixture metadata - not a real product specification"
        )
    }

    func test_search_aPageWithNoCursorIsTheLastPage() async throws {
        stub(#"{"models":[]}"#)

        let page = try await makeClient().searchCollectionModels(
            accessToken: "token", categoryID: "category_watches", query: "zzz", limit: 25, cursor: nil
        )

        XCTAssertTrue(page.models.isEmpty)
        XCTAssertNil(page.nextCursor)
        XCTAssertFalse(page.hasMore)
    }

    func test_search_surfacesTheStatusCodeButNoMachineCode() async throws {
        stub(#"{"error":"unknown category_id"}"#, statusCode: 400)

        do {
            _ = try await makeClient().searchCollectionModels(
                accessToken: "token", categoryID: "category_stamps", query: "", limit: 25, cursor: nil
            )
            XCTFail("a 400 must throw")
        } catch let error as CollectionAPIError {
            XCTAssertEqual(error.statusCode, 400)
            XCTAssertFalse(
                error.isUnknownCategory,
                "catalog sends prose, not a code — a client must not pretend otherwise"
            )
        }
    }
}

final class CatalogSearchStubURLProtocol: URLProtocol {
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
        let response = HTTPURLResponse(
            url: request.url!, statusCode: stub.statusCode, httpVersion: "HTTP/1.1", headerFields: nil
        )!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: stub.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

// MARK: - close-out: item writes declare the bundle-format generation

final class CollectionItemWriteClientTests: XCTestCase {
    override func tearDown() {
        CatalogSearchStubURLProtocol.stub = nil
        CatalogSearchStubURLProtocol.lastRequest = nil
        super.tearDown()
    }

    private func makeClient() -> CollectionAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CatalogSearchStubURLProtocol.self]
        return CollectionAPIClient(
            baseURL: URL(string: "https://example.invalid")!,
            session: URLSession(configuration: configuration)
        )
    }

    private let roomBody = """
        {"id":"c1","name":"Watches","category_id":"category_watches","design_id":"dev-fixture:collection-design",
         "current_tier":1,"items":[{"id":"i1","slot_index":0,"catalog_model_id":"dev-fixture:model-chrono-one"}],
         "created_at":"2026-08-26T00:00:00Z","updated_at":"2026-08-26T00:00:00Z"}
        """

    private func assetVersionQueryItem(of request: URLRequest?) -> String? {
        guard let url = request?.url,
              let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        return components.queryItems?.first { $0.name == "app_asset_version" }?.value
    }

    func test_addItemDeclaresTheSameGenerationAsAManifestRequest() async throws {
        CatalogSearchStubURLProtocol.stub = .init(statusCode: 201, body: Data(roomBody.utf8))

        _ = try await makeClient().addItem(
            accessToken: "t", collectionRoomID: "c1", catalogModelID: "dev-fixture:model-chrono-one"
        )

        let request = CatalogSearchStubURLProtocol.lastRequest
        XCTAssertEqual(request?.httpMethod, "POST")
        XCTAssertEqual(request?.url?.path, "/collection-rooms/c1/items")
        XCTAssertEqual(
            assetVersionQueryItem(of: request),
            String(AssetBundleFormat.appAssetVersion),
            "the item write must declare the generation the manifest request declares"
        )
    }

    func test_placeItemDeclaresTheSameGenerationAsAManifestRequest() async throws {
        CatalogSearchStubURLProtocol.stub = .init(statusCode: 200, body: Data(roomBody.utf8))

        _ = try await makeClient().placeItem(
            accessToken: "t", collectionRoomID: "c1", collectionItemID: "i1", slotIndex: 3
        )

        let request = CatalogSearchStubURLProtocol.lastRequest
        XCTAssertEqual(request?.httpMethod, "PUT")
        XCTAssertEqual(request?.url?.path, "/collection-rooms/c1/items/i1/slot")
        XCTAssertEqual(assetVersionQueryItem(of: request), String(AssetBundleFormat.appAssetVersion))
    }

    func test_slotNotAvailableIsRecognisedAsAClientSideRefusal() async {
        CatalogSearchStubURLProtocol.stub = .init(
            statusCode: 400,
            body: Data(#"{"error":"that slot is not available at the room's current tier","code":"slot_not_available"}"#.utf8)
        )

        do {
            _ = try await makeClient().placeItem(
                accessToken: "t", collectionRoomID: "c1", collectionItemID: "i1", slotIndex: 99
            )
            XCTFail("expected a refusal")
        } catch let error as CollectionAPIError {
            XCTAssertEqual(error.statusCode, 400)
            XCTAssertTrue(error.isSlotNotAvailable)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }
}
