import XCTest
@testable import MuseApp

final class PhotoUploadAPIClientTests: XCTestCase {

    private var session: URLSession!
    private var client: PhotoUploadAPIClient!

    override func setUp() {
        PhotoStubURLProtocol.reset()
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [PhotoStubURLProtocol.self]
        session = URLSession(configuration: config)
        client = PhotoUploadAPIClient(baseURL: URL(string: "https://api.test")!, session: session)
    }

    private func declaration(_ id: String = "picked_1") -> PhotoUploadDeclaration {
        PhotoUploadDeclaration(
            clientUploadID: id,
            file: NormalizedPhotoFile(
                fileURL: URL(fileURLWithPath: "/tmp/x.jpg"), contentType: "image/jpeg", byteSize: 4096,
                pixelWidth: 3072, pixelHeight: 2048, sha256Hex: String(repeating: "ab", count: 32)
            )
        )
    }

    // MARK: - Initiate

    func test_initiate_sendsTheExactDeclarationShape() async throws {
        PhotoStubURLProtocol.respond(status: 201, json: """
        {"asset_id":"a1","state":"pending","upload":{"url":"https://r2.test/photos/acct/a1?sig=x","method":"PUT",
         "headers":{"Content-Type":"image/jpeg","X-Amz-Checksum-Sha256":"q80="},"expires_at":"2026-08-25T10:00:00Z"}}
        """)

        let ticket = try await client.initiateUpload(accessToken: "tok", declaration: declaration())

        let request = PhotoStubURLProtocol.lastRequest!
        XCTAssertEqual(request.url?.path, "/media/photo-uploads")
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer tok")
        let body = try JSONSerialization.jsonObject(with: PhotoStubURLProtocol.lastBody!) as! [String: Any]
        XCTAssertEqual(body["client_upload_id"] as? String, "picked_1")
        XCTAssertEqual(body["content_type"] as? String, "image/jpeg")
        XCTAssertEqual(body["byte_size"] as? Int, 4096)
        XCTAssertEqual(body["pixel_width"] as? Int, 3072)
        XCTAssertEqual(body["pixel_height"] as? Int, 2048)
        XCTAssertEqual(body["checksum_sha256"] as? String, String(repeating: "ab", count: 32))
        XCTAssertEqual(Set(body.keys), ["client_upload_id", "content_type", "byte_size", "pixel_width", "pixel_height", "checksum_sha256"])

        XCTAssertEqual(ticket.assetID, "a1")
        XCTAssertFalse(ticket.isCommitted)
        XCTAssertEqual(ticket.upload?.url.absoluteString, "https://r2.test/photos/acct/a1?sig=x")
        XCTAssertEqual(ticket.upload?.method, "PUT")
        XCTAssertEqual(ticket.upload?.headers["X-Amz-Checksum-Sha256"], "q80=")
    }

    func test_initiate_committedAsset_hasNoUploadInstructions() async throws {
        PhotoStubURLProtocol.respond(status: 200, json: #"{"asset_id":"a1","state":"committed","upload":null}"#)

        let ticket = try await client.initiateUpload(accessToken: "tok", declaration: declaration())

        XCTAssertTrue(ticket.isCommitted)
        XCTAssertNil(ticket.upload)
    }

    func test_initiate_rejection_surfacesTheBackendsCode() async {
        PhotoStubURLProtocol.respond(status: 400, json: #"{"error":"photo exceeds the 10 MiB limit","code":"photo_too_large"}"#)

        do {
            _ = try await client.initiateUpload(accessToken: "tok", declaration: declaration())
            XCTFail("expected a PhotoAPIError")
        } catch let error as PhotoAPIError {
            XCTAssertEqual(error.statusCode, 400)
            XCTAssertEqual(error.code, "photo_too_large")
            XCTAssertEqual(error.message, "photo exceeds the 10 MiB limit")
            XCTAssertNil(error.assetID)
        } catch {
            XCTFail("unexpected \(error)")
        }
    }

    // MARK: - Assign

    func test_assign_sendsOrderedAssetIDs_andDecodesTheRoomsSlots() async throws {
        PhotoStubURLProtocol.respond(status: 201, json: """
        {"photo_slots":[{"slot_index":0,"photo_asset_id":"old","caption":""},{"slot_index":1,"photo_asset_id":"a1","caption":""},{"slot_index":2,"photo_asset_id":"a2","caption":""}]}
        """)

        let slots = try await client.assignPhotos(accessToken: "tok", roomID: "room-9", assetIDs: ["a1", "a2"])

        let request = PhotoStubURLProtocol.lastRequest!
        XCTAssertEqual(request.url?.path, "/museum/me/rooms/room-9/photos")
        let body = try JSONSerialization.jsonObject(with: PhotoStubURLProtocol.lastBody!) as! [String: Any]
        XCTAssertEqual(body["asset_ids"] as? [String], ["a1", "a2"])
        XCTAssertEqual(slots.map(\.slotIndex), [0, 1, 2])
        XCTAssertEqual(slots.map(\.photoAssetID), ["old", "a1", "a2"])
    }

    func test_assign_assetSpecificRefusal_namesTheAsset() async {
        PhotoStubURLProtocol.respond(status: 409, json: #"{"error":"photo bytes have not been uploaded yet","code":"asset_not_uploaded","asset_id":"a2"}"#)

        do {
            _ = try await client.assignPhotos(accessToken: "tok", roomID: "r", assetIDs: ["a1", "a2"])
            XCTFail("expected a PhotoAPIError")
        } catch let error as PhotoAPIError {
            XCTAssertEqual(error.statusCode, 409)
            XCTAssertEqual(error.code, "asset_not_uploaded")
            XCTAssertEqual(error.assetID, "a2")
        } catch {
            XCTFail("unexpected \(error)")
        }
    }

    // MARK: - RFC 3339 expiries (decoding fixed at the close-out)

    private let expiryShapes: [(text: String, offset: TimeInterval)] = [
        ("2026-08-26T22:29:58Z", 0),
        ("2026-08-26T22:29:58.454Z", 0.454),
        ("2026-08-27T01:29:58+03:00", 0),
        ("2026-08-27T01:29:58.052175+03:00", 0.052175),
    ]

    func test_initiate_decodesTheUploadExpiry_inEveryShapeTheServerProduces() async throws {
        let whole = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:29:58Z"))
        for shape in expiryShapes {
            PhotoStubURLProtocol.respond(status: 201, json: """
            {"asset_id":"a1","state":"pending","upload":{"url":"https://r2.test/photos/acct/a1?sig=x","method":"PUT","headers":{},"expires_at":"\(shape.text)"}}
            """)

            let ticket = try await client.initiateUpload(accessToken: "tok", declaration: declaration())

            let upload = try XCTUnwrap(ticket.upload, shape.text)
            XCTAssertEqual(upload.expiresAt.timeIntervalSince(whole), shape.offset, accuracy: 1e-6, shape.text)
        }
    }

    func test_fetchPhotoURLs_decodesTheTicketExpiry_inEveryShapeTheServerProduces() async throws {
        let whole = try XCTUnwrap(RFC3339Date.parse("2026-08-26T22:29:58Z"))
        for shape in expiryShapes {
            PhotoStubURLProtocol.respond(status: 200, json: """
            {"tickets":[{"photo_asset_id":"a1","url":"https://r2.test/photos/acct/a1?X-Amz-Signature=s","expires_at":"\(shape.text)","pixel_width":3072,"pixel_height":2048}]}
            """)

            let tickets = try await client.fetchPhotoURLs(accessToken: "tok", roomID: "room-9")

            XCTAssertEqual(tickets.count, 1, shape.text)
            XCTAssertEqual(tickets[0].expiresAt.timeIntervalSince(whole), shape.offset, accuracy: 1e-6, shape.text)
        }
    }

    func test_malformedExpiry_failsTheDecode_onBothRoutes() async {
        PhotoStubURLProtocol.respond(status: 201, json: """
        {"asset_id":"a1","state":"pending","upload":{"url":"https://r2.test/x","method":"PUT","headers":{},"expires_at":"not-a-date"}}
        """)
        do {
            _ = try await client.initiateUpload(accessToken: "tok", declaration: declaration())
            XCTFail("initiate: expected the decode to fail")
        } catch is DecodingError {
        } catch {
            XCTFail("initiate: expected a DecodingError, got \(error)")
        }

        PhotoStubURLProtocol.respond(status: 200, json: """
        {"tickets":[{"photo_asset_id":"a1","url":"https://r2.test/x","expires_at":"2026-08-26T22:29:58.Z","pixel_width":10,"pixel_height":10}]}
        """)
        do {
            _ = try await client.fetchPhotoURLs(accessToken: "tok", roomID: "room-9")
            XCTFail("tickets: expected the decode to fail")
        } catch is DecodingError {
        } catch {
            XCTFail("tickets: expected a DecodingError, got \(error)")
        }
    }

    // MARK: - Photo URLs (the seam)

    func test_fetchPhotoURLs_decodesTicketsWithDimensionsAndExpiry() async throws {
        PhotoStubURLProtocol.respond(status: 200, json: """
        {"tickets":[{"photo_asset_id":"a1","url":"https://r2.test/photos/acct/a1?X-Amz-Signature=s","expires_at":"2026-08-25T10:05:00Z","pixel_width":3072,"pixel_height":2048}]}
        """)

        let tickets = try await client.fetchPhotoURLs(accessToken: "tok", roomID: "room-9")

        XCTAssertEqual(PhotoStubURLProtocol.lastRequest?.url?.path, "/museum/me/rooms/room-9/photo-urls")
        XCTAssertEqual(tickets.count, 1)
        XCTAssertEqual(tickets[0].photoAssetID, "a1")
        XCTAssertEqual(tickets[0].pixelWidth, 3072)
        XCTAssertEqual(tickets[0].pixelHeight, 2048)
        XCTAssertEqual(tickets[0].url.host, "r2.test")
        XCTAssertEqual(tickets[0].expiresAt, ISO8601DateFormatter().date(from: "2026-08-25T10:05:00Z"))
    }

    // MARK: - Reorder ( — the contract consumes)

    func test_reorder_PUTsTheCompleteOrder_andDecodesTheAuthoritativeSlots() async throws {
        PhotoStubURLProtocol.respond(status: 200, json: """
        {"photo_slots":[{"slot_index":0,"photo_asset_id":"b","caption":"B"},{"slot_index":1,"photo_asset_id":"a","caption":"A"}]}
        """)

        let slots = try await client.reorderPhotos(accessToken: "tok", roomID: "room-9", orderedAssetIDs: ["b", "a"])

        let request = PhotoStubURLProtocol.lastRequest!
        XCTAssertEqual(request.httpMethod, "PUT")
        XCTAssertEqual(request.url?.path, "/museum/me/rooms/room-9/photo-order")
        let body = try JSONSerialization.jsonObject(with: PhotoStubURLProtocol.lastBody!) as! [String: Any]
        XCTAssertEqual(body["photo_asset_ids"] as? [String], ["b", "a"])
        XCTAssertEqual(Set(body.keys), ["photo_asset_ids"], "the body is the complete order and nothing else")

        XCTAssertEqual(slots.map(\.photoAssetID), ["b", "a"])
        XCTAssertEqual(slots.map(\.caption), ["B", "A"])
        XCTAssertEqual(slots.map(\.slotIndex), [0, 1])
    }

    func test_reorder_staleOrder_surfacesTheMismatchCode() async {
        PhotoStubURLProtocol.respond(status: 409, json: #"{"error":"photo order does not match the room's current photographs; reload and retry","code":"order_mismatch"}"#)

        do {
            _ = try await client.reorderPhotos(accessToken: "tok", roomID: "r", orderedAssetIDs: ["a", "b"])
            XCTFail("expected a PhotoAPIError")
        } catch let error as PhotoAPIError {
            XCTAssertEqual(error.statusCode, 409)
            XCTAssertEqual(error.code, "order_mismatch")
        } catch {
            XCTFail("unexpected \(error)")
        }
    }

    // MARK: - Replace

    func test_replace_POSTsTheReplacementAssetID_andDecodesTheAuthoritativeSlots() async throws {
        PhotoStubURLProtocol.respond(status: 200, json: """
        {"photo_slots":[{"slot_index":0,"photo_asset_id":"a","caption":""},{"slot_index":1,"photo_asset_id":"new","caption":"kept"},{"slot_index":2,"photo_asset_id":"c","caption":""}]}
        """)

        let slots = try await client.replacePhoto(accessToken: "tok", roomID: "room-9", photoAssetID: "b", replacementAssetID: "new")

        let request = PhotoStubURLProtocol.lastRequest!
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/museum/me/rooms/room-9/photos/b/replacement", "addressed by the current photograph")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer tok")
        let body = try JSONSerialization.jsonObject(with: PhotoStubURLProtocol.lastBody!) as! [String: Any]
        XCTAssertEqual(body["asset_id"] as? String, "new")
        XCTAssertEqual(Set(body.keys), ["asset_id"], "the body is the replacement and nothing else")

        XCTAssertEqual(slots.map(\.photoAssetID), ["a", "new", "c"])
        XCTAssertEqual(slots.map(\.caption), ["", "kept", ""])
        XCTAssertEqual(slots.map(\.slotIndex), [0, 1, 2])
    }

    func test_replace_refusal_surfacesTheBackendsCode() async {
        PhotoStubURLProtocol.respond(status: 409, json: #"{"error":"photo is already assigned to a room","code":"asset_already_assigned","asset_id":"new"}"#)

        do {
            _ = try await client.replacePhoto(accessToken: "tok", roomID: "r", photoAssetID: "b", replacementAssetID: "new")
            XCTFail("expected a PhotoAPIError")
        } catch let error as PhotoAPIError {
            XCTAssertEqual(error.statusCode, 409)
            XCTAssertEqual(error.code, "asset_already_assigned")
            XCTAssertEqual(error.assetID, "new")
        } catch {
            XCTFail("unexpected \(error)")
        }
    }

    // MARK: - Delete

    func test_delete_DELETEsThePhotograph_withNoBody_andDecodesTheCompactedSlots() async throws {
        PhotoStubURLProtocol.respond(status: 200, json: """
        {"photo_slots":[{"slot_index":0,"photo_asset_id":"a","caption":"A"},{"slot_index":1,"photo_asset_id":"c","caption":"C"}]}
        """)

        let slots = try await client.deletePhoto(accessToken: "tok", roomID: "room-9", photoAssetID: "b")

        let request = PhotoStubURLProtocol.lastRequest!
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/museum/me/rooms/room-9/photos/b", "addressed by the photograph")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer tok")
        XCTAssertNil(PhotoStubURLProtocol.lastBody.flatMap { $0.isEmpty ? nil : $0 }, "a deletion carries no body")

        XCTAssertEqual(slots.map(\.photoAssetID), ["a", "c"])
        XCTAssertEqual(slots.map(\.slotIndex), [0, 1])
        XCTAssertEqual(slots.map(\.caption), ["A", "C"])
    }

    func test_delete_notInRoom_surfacesTheCode() async {
        PhotoStubURLProtocol.respond(status: 404, json: #"{"error":"photo is not in this room","code":"photo_not_in_room"}"#)

        do {
            _ = try await client.deletePhoto(accessToken: "tok", roomID: "r", photoAssetID: "b")
            XCTFail("expected a PhotoAPIError")
        } catch let error as PhotoAPIError {
            XCTAssertEqual(error.statusCode, 404)
            XCTAssertEqual(error.code, "photo_not_in_room")
        } catch {
            XCTFail("unexpected \(error)")
        }
    }

    // MARK: - Storage PUT

    func test_objectUploader_sendsExactlyTheSignedHeaders_andNoAuthorization() async throws {
        PhotoStubURLProtocol.respond(status: 200, json: "")
        let file = FileManager.default.temporaryDirectory.appendingPathComponent("put-\(UUID().uuidString).jpg")
        try Data(repeating: 7, count: 128).write(to: file)
        defer { try? FileManager.default.removeItem(at: file) }

        let instructions = PhotoUploadTicket.UploadInstructions(
            url: URL(string: "https://r2.test/bucket/photos/acct/a1?X-Amz-Signature=abc")!,
            method: "PUT",
            headers: ["Content-Type": "image/jpeg", "X-Amz-Checksum-Sha256": "q80="],
            expiresAt: Date().addingTimeInterval(300)
        )
        try await URLSessionObjectUploader(session: session).upload(file: file, using: instructions)

        let request = PhotoStubURLProtocol.lastRequest!
        XCTAssertEqual(request.httpMethod, "PUT")
        XCTAssertEqual(request.url?.absoluteString, instructions.url.absoluteString)
        XCTAssertEqual(request.value(forHTTPHeaderField: "Content-Type"), "image/jpeg")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Amz-Checksum-Sha256"), "q80=")
        XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"), "storage traffic carries no Muse bearer token")
    }

    func test_objectUploader_nonSuccess_throws() async throws {
        PhotoStubURLProtocol.respond(status: 403, json: "")
        let file = FileManager.default.temporaryDirectory.appendingPathComponent("put-\(UUID().uuidString).jpg")
        try Data([1]).write(to: file)
        defer { try? FileManager.default.removeItem(at: file) }

        let instructions = PhotoUploadTicket.UploadInstructions(url: URL(string: "https://r2.test/x")!, method: "PUT", headers: [:], expiresAt: Date())
        do {
            try await URLSessionObjectUploader(session: session).upload(file: file, using: instructions)
            XCTFail("a refused PUT must throw")
        } catch let error as PhotoAPIError {
            XCTAssertEqual(error.statusCode, 403)
        }
    }
}

final class PhotoStubURLProtocol: URLProtocol {
    nonisolated(unsafe) private static var status = 200
    nonisolated(unsafe) private static var body = Data()
    nonisolated(unsafe) private static var _lastRequest: URLRequest?
    nonisolated(unsafe) private static var _lastBody: Data?
    private static let lock = NSLock()

    static var lastRequest: URLRequest? { lock.withLock { _lastRequest } }
    static var lastBody: Data? { lock.withLock { _lastBody } }

    static func reset() {
        lock.withLock { status = 200; body = Data(); _lastRequest = nil; _lastBody = nil }
    }

    static func respond(status: Int, json: String) {
        lock.withLock { self.status = status; self.body = Data(json.utf8) }
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lock.withLock {
            Self._lastRequest = request
            Self._lastBody = request.httpBody ?? request.httpBodyStream.map { stream in
                stream.open()
                defer { stream.close() }
                var data = Data()
                let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: 4096)
                defer { buffer.deallocate() }
                while stream.hasBytesAvailable {
                    let read = stream.read(buffer, maxLength: 4096)
                    if read <= 0 { break }
                    data.append(buffer, count: read)
                }
                return data
            }
        }
        let (status, body) = Self.lock.withLock { (Self.status, Self.body) }
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
