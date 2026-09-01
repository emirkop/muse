import UIKit
import XCTest
@testable import MuseApp

// MARK: - Runtime: two visitors in one Room

@MainActor
final class ConcurrentVisitorRuntimeIsolationTests: XCTestCase {
    private func visitorController(photoCount: Int = 3) -> RealityKitSceneViewController {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: .visitor, room: fixture.room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures
        )
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        return controller
    }

    func test_twoVisitors_haveIndependentCamerasAndPositions() {
        let a = visitorController()
        let b = visitorController()
        let start = b.movementController.subject

        a.testMoveViewer(to: MuseumCameraSubject(position: SIMD3<Float>(1.2, 0, -0.8), yaw: 0.7))
        a.testAdvanceFrames(10)

        XCTAssertNotEqual(a.movementController.subject, start, "the moved visitor must actually have moved")
        XCTAssertEqual(b.movementController.subject, start, "the other visitor did not move")
        XCTAssertNotNil(a.cameraController)
        XCTAssertNotNil(b.cameraController)
        XCTAssertFalse(a.cameraController === b.cameraController, "each visitor owns a camera")
    }

    func test_twoVisitors_ownDistinctRenderingLayers() {
        let a = visitorController()
        let b = visitorController()

        XCTAssertNotNil(a.photoLayer)
        XCTAssertNotNil(b.photoLayer)
        XCTAssertFalse(a.photoLayer === b.photoLayer)
        XCTAssertFalse(a.captionLayer === b.captionLayer)
        XCTAssertFalse(a.sculptureLayer === b.sculptureLayer)
        XCTAssertNil(a.editMode)
        XCTAssertNil(b.editMode)
        XCTAssertNil(a.contentCoordinator)
        XCTAssertNil(b.contentCoordinator)
    }

    func test_movementStateIsAValue_notASharedReference() {
        var a = MuseumMovementController()
        let b = a

        a.update(input: MovementInput(forward: 1), deltaTime: 0.5)

        XCTAssertNotEqual(a.subject, b.subject)
        XCTAssertEqual(b.subject, MuseumMovementController().subject)
    }
}

// MARK: - Textures: two visitors' photograph streams

private actor IsolationTicketSource: RoomPhotoTicketing {
    private(set) var fetchesByRoom: [String: Int] = [:]
    private let tickets: [String: [PhotoDownloadTicket]]

    init(tickets: [String: [PhotoDownloadTicket]]) {
        self.tickets = tickets
    }

    func fetchPhotoURLs(accessToken: String, roomID: String) async throws -> [PhotoDownloadTicket] {
        fetchesByRoom[roomID, default: 0] += 1
        guard let mine = tickets[roomID] else {
            throw IdentityAPIClientError.server(statusCode: 404, message: "not found")
        }
        return mine
    }
}

private struct TinyJPEGDownloader: PhotoBytesDownloading {
    let data: Data

    static func make() -> TinyJPEGDownloader {
        let renderer = UIGraphicsImageRenderer(size: CGSize(width: 16, height: 12))
        let image = renderer.image { context in
            UIColor.systemTeal.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 16, height: 12))
        }
        return TinyJPEGDownloader(data: image.jpegData(compressionQuality: 0.8)!)
    }

    func download(_ url: URL) async throws -> Data { data }
}

final class ConcurrentVisitorTextureIsolationTests: XCTestCase {
    private static func placements(_ count: Int) -> [ResolvedPhotoPlacement] {
        (0..<count).map { slot in
            ResolvedPhotoPlacement(
                slotIndex: slot, photoAssetID: "asset_\(slot)", caption: "",
                anchor: SlotAnchor(wall: .left, positionOnWall: slot),
                transform: SlotTransform(position: .zero)
            )
        }
    }

    private static func tickets(_ count: Int) -> [PhotoDownloadTicket] {
        (0..<count).map {
            PhotoDownloadTicket(
                photoAssetID: "asset_\($0)",
                url: URL(string: "https://cdn.example/asset_\($0)")!,
                expiresAt: Date(timeIntervalSinceNow: 300),
                pixelWidth: 16, pixelHeight: 12
            )
        }
    }

    private static func collect(_ stream: AsyncStream<RoomPhotoTextureEvent>) async -> [RoomPhotoTextureEvent] {
        var events: [RoomPhotoTextureEvent] = []
        for await event in stream { events.append(event) }
        return events
    }

    private func decodedSlots(_ events: [RoomPhotoTextureEvent]) -> Set<Int> {
        Set(events.compactMap { if case .decoded(let s, _) = $0 { return s } else { return nil } })
    }

    private func failures(_ events: [RoomPhotoTextureEvent]) -> [RoomPhotoLoadFailure] {
        events.compactMap { if case .failed(_, let r) = $0 { return r } else { return nil } }
    }

    func test_twoConcurrentStreams_shareNoTicketState() async {
        let source = IsolationTicketSource(tickets: ["room-live": Self.tickets(3)])
        let loader = RoomPhotoTextureLoader(photoService: source, downloader: TinyJPEGDownloader.make())
        let liveStream = loader.textures(for: Self.placements(3), roomID: "room-live", accessToken: "visitor-a", maxLongEdge: 64)
        let deadStream = loader.textures(for: Self.placements(3), roomID: "room-dead", accessToken: "visitor-b", maxLongEdge: 64)

        async let live = Self.collect(liveStream)
        async let dead = Self.collect(deadStream)
        let (liveEvents, deadEvents) = await (live, dead)

        XCTAssertEqual(decodedSlots(liveEvents), [0, 1, 2], "the admitted visitor sees every photograph")
        XCTAssertTrue(failures(liveEvents).isEmpty)
        XCTAssertEqual(failures(deadEvents), [.noTicket, .noTicket, .noTicket], "the refused visitor gets the one refusal, per photograph")
        XCTAssertTrue(decodedSlots(deadEvents).isEmpty)

        let fetches = await source.fetchesByRoom
        XCTAssertEqual(fetches, ["room-live": 1, "room-dead": 1], "each stream fetched for itself, once")
    }

    func test_twoStreamsForTheSameRoom_eachFetchTheirOwnTickets() async {
        let source = IsolationTicketSource(tickets: ["room": Self.tickets(2)])
        let loader = RoomPhotoTextureLoader(photoService: source, downloader: TinyJPEGDownloader.make())
        let streamA = loader.textures(for: Self.placements(2), roomID: "room", accessToken: "visitor-a", maxLongEdge: 64)
        let streamB = loader.textures(for: Self.placements(2), roomID: "room", accessToken: "visitor-b", maxLongEdge: 64)

        async let first = Self.collect(streamA)
        async let second = Self.collect(streamB)
        let (a, b) = await (first, second)

        XCTAssertEqual(decodedSlots(a), [0, 1])
        XCTAssertEqual(decodedSlots(b), [0, 1])
        let fetches = await source.fetchesByRoom
        XCTAssertEqual(fetches["room"], 2, "no ticket cache is shared across streams")
    }
}

// MARK: - Presentation: two visits, two view models, two apps

@MainActor
final class ConcurrentVisitorPresentationIsolationTests: XCTestCase {
    private struct NoLobbyGeometry: LobbyGeometryProviding {
        func lobbyGeometry(forStyleID styleID: String) async -> LobbyRuntimeContent.Geometry? { nil }
    }

    private struct NoCardTables: LobbyCardTableProviding {
        func cardTable(forStyleID styleID: String, cardCount: Int) async -> LobbyCardTable? { nil }
    }

    func test_twoLobbyEntries_holdIndependentState() async {
        let service = FakeShareLinkService()
        service.sharedMuseumResult = .success(SharedMuseumContent(
            museumID: "m1", styleID: "style_modern",
            rooms: [SharedRoomSummary(id: "r1", name: "Only", variantID: "v1")]
        ))
        let make = {
            LobbyEntryViewModel(
                viewerRole: .visitor,
                contentSource: SharedMuseumLobbyContent(shareLinkService: service, code: "abcdefghijklmnopqrstuv"),
                geometry: NoLobbyGeometry(), cardTables: NoCardTables(), accessToken: "token"
            )
        }
        let early = make()
        let late = make()

        await early.load()
        service.sharedMuseumResult = .failure(IdentityAPIClientError.server(statusCode: 404, message: "not found"))
        await late.load()

        guard case .enterRoomDirectly = early.state else { return XCTFail("early visitor entered: \(early.state)") }
        XCTAssertEqual(late.state, .noLongerAvailable, "the late visitor is refused")
        guard case .enterRoomDirectly = early.state else { return XCTFail("the early visitor's state must be untouched by the late one's refusal") }
    }

    func test_twoCoordinators_holdIndependentPendingLinks() {
        UIView.setAnimationsEnabled(false)
        defer { UIView.setAnimationsEnabled(true) }
        let scene = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first
        let makeCoordinator = { AppCoordinator(window: scene.map(UIWindow.init(windowScene:)) ?? UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 852))) }
        let a = makeCoordinator()
        let b = makeCoordinator()

        XCTAssertTrue(a.handleIncomingURL(URL(string: "https://muse.app/m/abcdefghijklmnopqrstuv")!))

        XCTAssertEqual(a.testPendingShareLinkCode, "abcdefghijklmnopqrstuv")
        XCTAssertNil(b.testPendingShareLinkCode, "another app instance knows nothing of this link")
    }
}
