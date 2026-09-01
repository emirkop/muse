import XCTest
@testable import MuseApp

final class CollectionMusicClientTests: XCTestCase {
    override func tearDown() {
        CollectionMusicStubURLProtocol.stub = nil
        CollectionMusicStubURLProtocol.lastRequest = nil
        CollectionMusicStubURLProtocol.lastBody = nil
        super.tearDown()
    }

    private func makeClient() -> CollectionAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CollectionMusicStubURLProtocol.self]
        return CollectionAPIClient(baseURL: URL(string: "https://example.invalid")!, session: URLSession(configuration: configuration))
    }

    private func stub(_ json: String, statusCode: Int = 200) {
        CollectionMusicStubURLProtocol.stub = .init(statusCode: statusCode, body: Data(json.utf8))
    }

    private let roomJSON = #"{"id":"cr-1","name":"Watches","category_id":"category_watches","design_id":"","current_tier":1,"music_track_id":"track_dev_a","items":[],"created_at":"2026-08-27T01:29:58.052175+03:00","updated_at":"2026-08-27T01:29:58.052175+03:00"}"#

    func test_assign_putsTheBody_toTheCollectionRoute_andDecodesTheRoom() async throws {
        stub(roomJSON)

        let room = try await makeClient().assignCollectionRoomMusic(accessToken: "token", collectionRoomID: "cr-1", musicTrackID: "track_dev_a")

        XCTAssertEqual(room.musicTrackID, "track_dev_a")
        XCTAssertTrue(room.hasMusic)
        XCTAssertEqual(room.name, "Watches")
        let request = try XCTUnwrap(CollectionMusicStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/collection-rooms/cr-1/music")
        XCTAssertEqual(request.httpMethod, "PUT")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer token")
        let body = try JSONDecoder().decode([String: String].self, from: try XCTUnwrap(CollectionMusicStubURLProtocol.lastBody))
        XCTAssertEqual(body, ["music_track_id": "track_dev_a"], "the same body shape as museum's PUT …/music")
    }

    func test_remove_deletesOnTheCollectionRoute_andDecodesNoMusic() async throws {
        stub(#"{"id":"cr-1","name":"Watches","category_id":"category_watches","design_id":"","current_tier":1,"items":[]}"#)

        let room = try await makeClient().removeCollectionRoomMusic(accessToken: "token", collectionRoomID: "cr-1")

        XCTAssertNil(room.musicTrackID, "the server omits music_track_id for no music; the Domain says nil")
        XCTAssertFalse(room.hasMusic)
        let request = try XCTUnwrap(CollectionMusicStubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/collection-rooms/cr-1/music")
        XCTAssertEqual(request.httpMethod, "DELETE")
    }

    func test_unknownTrack_surfacesTheCode() async {
        stub(#"{"error":"music track is not in the catalog","code":"unknown_music_track"}"#, statusCode: 400)
        do {
            _ = try await makeClient().assignCollectionRoomMusic(accessToken: "token", collectionRoomID: "cr-1", musicTrackID: "track_nope")
            XCTFail("expected a refusal")
        } catch let error as CollectionAPIError {
            XCTAssertEqual(error.statusCode, 400)
            XCTAssertEqual(error.code, "unknown_music_track")
        } catch {
            XCTFail("expected CollectionAPIError, got \(error)")
        }
    }

    func test_fetchRoom_decodesTheAssignment() async throws {
        stub(roomJSON)
        let room = try await makeClient().fetchCollectionRoom(accessToken: "token", collectionRoomID: "cr-1")
        XCTAssertEqual(room.musicTrackID, "track_dev_a")
    }

    func test_visitorPayload_musicIsPresentOnlyWhenTheServerSendsIt() async throws {
        stub(#"{"collection_room_id":"cr-1","name":"Shared Watches","category_id":"category_watches","design_id":"","current_tier":1,"items":[]}"#)
        let gated = try await makeClient().sharedCollectionRoom(accessToken: "token", code: "abcdefghijklmnopqrstuv")
        XCTAssertNil(gated.musicTrackID)

        stub(#"{"collection_room_id":"cr-1","name":"Shared Watches","category_id":"category_watches","design_id":"","current_tier":1,"music_track_id":"track_dev_a","items":[]}"#)
        let cleared = try await makeClient().sharedCollectionRoom(accessToken: "token", code: "abcdefghijklmnopqrstuv")
        XCTAssertEqual(cleared.musicTrackID, "track_dev_a")
    }
}

private final class CollectionMusicStubURLProtocol: URLProtocol {
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
