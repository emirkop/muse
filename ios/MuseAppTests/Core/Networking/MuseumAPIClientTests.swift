import XCTest
@testable import MuseApp

final class MuseumAPIClientTests: XCTestCase {
    override func tearDown() {
        MuseumStubURLProtocol.stub = nil
        MuseumStubURLProtocol.lastRequest = nil
        MuseumStubURLProtocol.lastRequestBody = nil
        super.tearDown()
    }

    private func makeClient() -> MuseumAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MuseumStubURLProtocol.self]
        return MuseumAPIClient(baseURL: URL(string: "https://example.invalid")!, session: URLSession(configuration: configuration))
    }

    private func stub(_ json: String, statusCode: Int = 200) {
        MuseumStubURLProtocol.stub = .init(statusCode: statusCode, body: Data(json.utf8))
    }

    // MARK: - Music audio URL expiry (; decoding fixed at the close-out)

    func test_musicAudioURL_decodesExpiresAt_inEveryShapeTheServerProduces() async throws {
        let whole = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:29:58Z"))
        let shapes: [(text: String, offset: TimeInterval)] = [
            ("2026-08-26T22:29:58Z", 0),
            ("2026-08-26T22:29:58.454Z", 0.454),
            ("2026-08-27T01:29:58+03:00", 0),
            ("2026-08-27T01:29:58.052175+03:00", 0.052175),
        ]
        for shape in shapes {
            stub(#"{"url":"https://cdn.example/audio/t1?sig=x","expires_at":"\#(shape.text)"}"#)

            let audio = try await makeClient().musicAudioURL(accessToken: "token", trackID: "t1")

            XCTAssertEqual(audio.expiresAt.timeIntervalSince(whole), shape.offset, accuracy: 1e-6, shape.text)
            XCTAssertEqual(audio.url.absoluteString, "https://cdn.example/audio/t1?sig=x")
            XCTAssertEqual(MuseumStubURLProtocol.lastRequest?.url?.path, "/catalog/music/t1/audio-url")
        }
    }

    func test_musicAudioURL_malformedExpiry_failsTheDecode() async {
        stub(#"{"url":"https://cdn.example/audio/t1","expires_at":"2026-08-26 22:29:58"}"#)
        do {
            _ = try await makeClient().musicAudioURL(accessToken: "token", trackID: "t1")
            XCTFail("expected the decode to fail")
        } catch is DecodingError {
        } catch {
            XCTFail("expected a DecodingError, got \(error)")
        }
    }

    // MARK: - Museum

    func test_createMuseum_sendsStyleReference_andDecodesMuseum() async throws {
        stub(#"{"id":"m1","style_id":"style_modern","privacy":"private"}"#, statusCode: 201)

        let museum = try await makeClient().createMuseum(accessToken: "token", styleID: "style_modern")

        XCTAssertEqual(museum, Museum(id: "m1", styleID: "style_modern", privacy: .private))

        let request = try XCTUnwrap(MuseumStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/museum")
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token")

        let body = try JSONDecoder().decode([String: String].self, from: try XCTUnwrap(MuseumStubURLProtocol.lastRequestBody))
        XCTAssertEqual(body["style_id"], "style_modern")
    }

    func test_createMuseum_whenOneAlreadyExists_surfacesConflict() async {
        stub(#"{"error":"account already owns a museum"}"#, statusCode: 409)

        do {
            _ = try await makeClient().createMuseum(accessToken: "token", styleID: "style_modern")
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, _) {
            XCTAssertEqual(statusCode, 409)
        } catch {
            XCTFail("expected .server, got \(error)")
        }
    }

    func test_changeStyle_targetsTheStyleEndpoint_andReturnsUpdatedReference() async throws {
        stub(#"{"id":"m1","style_id":"style_gothic","privacy":"private"}"#)

        let museum = try await makeClient().changeStyle(accessToken: "token", styleID: "style_gothic")

        XCTAssertEqual(museum.styleID, "style_gothic")
        let request = try XCTUnwrap(MuseumStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/museum/me/style")
        XCTAssertEqual(request.httpMethod, "PATCH")
    }

    // MARK: - Rooms

    func test_listRooms_decodesLogicalSlots_withNoTransformData() async throws {
        stub("""
        {"rooms":[{
            "id":"r1","name":"The Long Hall","variant_id":"style_modern_variant_Hall","privacy":"public",
            "photo_slots":[{"slot_index":0,"photo_asset_id":"asset_a","caption":"first"}],
            "sculptures":[{"slot_index":0,"catalog_id":"sculpture_x"}]
        }]}
        """)

        let rooms = try await makeClient().listRooms(accessToken: "token")

        let room = try XCTUnwrap(rooms.first)
        XCTAssertEqual(room.name, "The Long Hall")
        XCTAssertEqual(room.variantID, "style_modern_variant_Hall")
        XCTAssertEqual(room.privacy, .public)
        XCTAssertEqual(room.photoSlots.first?.slotIndex, 0)
        XCTAssertEqual(room.photoSlots.first?.caption, "first")
        XCTAssertEqual(room.sculptures.first?.catalogID, "sculpture_x")
    }

    func test_createRoom_sendsVariantReference() async throws {
        stub(#"{"id":"r1","name":"Hall","variant_id":"v1","privacy":"private","photo_slots":[],"sculptures":[]}"#, statusCode: 201)

        _ = try await makeClient().createRoom(accessToken: "token", name: "Hall", variantID: "v1")

        let body = try JSONDecoder().decode([String: String].self, from: try XCTUnwrap(MuseumStubURLProtocol.lastRequestBody))
        XCTAssertEqual(body["variant_id"], "v1")
        XCTAssertEqual(body["name"], "Hall")
    }

    func test_createRoom_withMismatchedVariant_surfacesBadRequest() async {
        stub(#"{"error":"variant does not belong to the museum's style"}"#, statusCode: 400)

        do {
            _ = try await makeClient().createRoom(accessToken: "token", name: "X", variantID: "gothic_variant")
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, let message) {
            XCTAssertEqual(statusCode, 400)
            XCTAssertEqual(message, "variant does not belong to the museum's style")
        } catch {
            XCTFail("expected .server, got \(error)")
        }
    }

    // MARK: - Privacy

    func test_changePrivacy_targetsThePrivacyEndpoint_andSendsOnlyPrivacy() async throws {
        stub(#"{"id":"m1","style_id":"style_modern","privacy":"public"}"#)

        let museum = try await makeClient().changePrivacy(accessToken: "token", privacy: .public)

        XCTAssertEqual(museum.privacy, .public)
        XCTAssertEqual(museum.styleID, "style_modern", "a privacy change must not move the Style reference")
        let request = try XCTUnwrap(MuseumStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/museum/me/privacy")
        XCTAssertEqual(request.httpMethod, "PATCH")
        let body = try XCTUnwrap(
            try JSONSerialization.jsonObject(
                with: try XCTUnwrap(MuseumStubURLProtocol.lastRequestBody)) as? [String: Any])
        XCTAssertEqual(body["privacy"] as? String, "public")
        XCTAssertEqual(body.count, 1, "the Museum-level control carries privacy and nothing else")
    }

    func test_updateRoom_privacyOnlyPatch_sendsOnlyThePrivacyKey() async throws {
        stub(#"{"id":"r1","name":"The Long Hall","variant_id":"v1","privacy":"public","photo_slots":[],"sculptures":[]}"#)

        _ = try await makeClient().updateRoom(accessToken: "token", roomID: "r1", patch: .privacy(.public))

        let request = try XCTUnwrap(MuseumStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/museum/me/rooms/r1")
        XCTAssertEqual(request.httpMethod, "PATCH")
        let body = try XCTUnwrap(
            try JSONSerialization.jsonObject(
                with: try XCTUnwrap(MuseumStubURLProtocol.lastRequestBody)) as? [String: Any])
        XCTAssertEqual(body["privacy"] as? String, "public")
        XCTAssertFalse(body.keys.contains("name"), "an omitted field must not appear at all: \(body)")
        XCTAssertFalse(body.keys.contains("variant_id"), "an omitted field must not appear at all: \(body)")
    }

    func test_updateRoom_variantOnlyPatch_sendsOnlyTheVariantKey() async throws {
        stub(#"{"id":"r1","name":"The Long Hall","variant_id":"v2","privacy":"private","photo_slots":[],"sculptures":[]}"#)

        _ = try await makeClient().updateRoom(accessToken: "token", roomID: "r1", patch: .variant("v2"))

        let body = try XCTUnwrap(
            try JSONSerialization.jsonObject(
                with: try XCTUnwrap(MuseumStubURLProtocol.lastRequestBody)) as? [String: Any])
        XCTAssertEqual(body["variant_id"] as? String, "v2")
        XCTAssertFalse(body.keys.contains("privacy"), "a design change must not carry privacy: \(body)")
        XCTAssertFalse(body.keys.contains("name"))
    }

    func test_updateRoom_whenTheServerRefuses_surfacesTheCollapsedNotFound() async {
        stub(#"{"error":"not found"}"#, statusCode: 404)

        do {
            _ = try await makeClient().updateRoom(accessToken: "token", roomID: "r1", patch: .privacy(.public))
            XCTFail("expected an error")
        } catch IdentityAPIClientError.server(let statusCode, _) {
            XCTAssertEqual(statusCode, 404)
        } catch {
            XCTFail("expected .server, got \(error)")
        }
    }

    // MARK: - Sculptures

    func test_fetchSculptures_decodesAnEmptyCatalog() async throws {
        stub(#"{"sculptures":[]}"#)

        let sculptures = try await makeClient().fetchSculptures(accessToken: "token")

        XCTAssertEqual(MuseumStubURLProtocol.lastRequest?.url?.path, "/catalog/sculptures")
        XCTAssertEqual(MuseumStubURLProtocol.lastRequest?.httpMethod, "GET")
        XCTAssertTrue(sculptures.isEmpty)
    }

    func test_fetchSculptures_decodesEntries_withAssetBundleVersions() async throws {
        stub("""
        {"sculptures":[{"id":"sculpture_a","display_name":"A","asset_bundle_id":"bundle_a","asset_bundle_version":3}]}
        """)

        let sculptures = try await makeClient().fetchSculptures(accessToken: "token")

        XCTAssertEqual(sculptures.count, 1)
        XCTAssertEqual(sculptures[0].id, "sculpture_a")
        XCTAssertEqual(sculptures[0].displayName, "A")
        XCTAssertEqual(sculptures[0].assetBundle, AssetBundleRef(id: "bundle_a", version: 3))
    }

    func test_addSculpture_sendsOnlyTheCatalogID_andDecodesTheAuthoritativeSlots() async throws {
        stub(#"{"sculptures":[{"slot_index":0,"catalog_id":"sculpture_a"},{"slot_index":2,"catalog_id":"sculpture_c"}]}"#, statusCode: 201)

        let sculptures = try await makeClient().addSculpture(accessToken: "token", roomID: "room-9", catalogID: "sculpture_a")

        let request = try XCTUnwrap(MuseumStubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/museum/me/rooms/room-9/sculptures")
        let body = try JSONDecoder().decode([String: String].self, from: try XCTUnwrap(MuseumStubURLProtocol.lastRequestBody))
        XCTAssertEqual(body, ["catalog_id": "sculpture_a"], "the slot is the server's to choose — the client never sends one")

        XCTAssertEqual(sculptures.map(\.slotIndex), [0, 2])
        XCTAssertEqual(sculptures.map(\.catalogID), ["sculpture_a", "sculpture_c"])
    }

    func test_removeSculpture_addressesTheSlot_andCarriesNoBody() async throws {
        stub(#"{"sculptures":[{"slot_index":0,"catalog_id":"sculpture_a"}]}"#)

        let sculptures = try await makeClient().removeSculpture(accessToken: "token", roomID: "room-9", slotIndex: 2)

        let request = try XCTUnwrap(MuseumStubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/museum/me/rooms/room-9/sculptures/2")
        XCTAssertNil(MuseumStubURLProtocol.lastRequestBody.flatMap { $0.isEmpty ? nil : $0 })
        XCTAssertEqual(sculptures.map(\.slotIndex), [0])
    }

    // MARK: - Catalog

    func test_fetchStyles_decodesPresentationModels_withAssetBundleVersions() async throws {
        stub("""
        {"styles":[
            {"id":"style_modern","display_name":"Modern","asset_bundle_id":"b1","asset_bundle_version":3}
        ]}
        """)

        let styles = try await makeClient().fetchStyles(accessToken: "token")

        let style = try XCTUnwrap(styles.first)
        XCTAssertEqual(style.displayName, "Modern")
        XCTAssertEqual(style.assetBundle, AssetBundleRef(id: "b1", version: 3))
    }

    func test_fetchVariants_scopesToTheRequestedStyle() async throws {
        stub("""
        {"variants":[
            {"id":"v1","style_id":"style_modern","display_name":"Hall","asset_bundle_id":"b2","asset_bundle_version":1}
        ]}
        """)

        let variants = try await makeClient().fetchVariants(accessToken: "token", styleID: "style_modern")

        XCTAssertEqual(variants.first?.styleID, "style_modern")
        let request = try XCTUnwrap(MuseumStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/catalog/styles/style_modern/variants")
    }

    // MARK: - Client-side capacity pre-check

    func test_roomCapacityHelpers_matchTheConfirmedCaps() {
        XCTAssertEqual(Room.maxPhotos, 28)
        XCTAssertEqual(Room.maxSculptures, 3)

        let full = Room(
            id: "r", name: "", variantID: "v", privacy: .private,
            photoSlots: (0..<28).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "", caption: "") },
            sculptures: (0..<3).map { SculptureInstance(slotIndex: $0, catalogID: "") }
        )
        XCTAssertFalse(full.hasCapacityForPhoto)
        XCTAssertFalse(full.hasCapacityForSculpture)

        let empty = Room(id: "r", name: "", variantID: "v", privacy: .private)
        XCTAssertTrue(empty.hasCapacityForPhoto)
        XCTAssertTrue(empty.hasCapacityForSculpture)
    }
}

private final class MuseumStubURLProtocol: URLProtocol {
    struct Stub {
        let statusCode: Int
        let body: Data
    }

    nonisolated(unsafe) static var stub: Stub?
    nonisolated(unsafe) static var lastRequest: URLRequest?
    nonisolated(unsafe) static var lastRequestBody: Data?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lastRequest = request
        Self.lastRequestBody = request.httpBody ?? Self.readBody(from: request.httpBodyStream)

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

    private static func readBody(from stream: InputStream?) -> Data? {
        guard let stream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: 1024)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }
}
