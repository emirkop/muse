import UIKit
import XCTest
@testable import MuseApp

// MARK: - Fakes

private final class FakeCollectionRoomMusicService: CollectionRoomMusicServicing, @unchecked Sendable {
    var knownTracks: Set<String> = ["track_dev_a", "track_dev_b"]
    private(set) var rooms: [String: CollectionRoom] = [:]
    private(set) var assignCalls: [(roomID: String, trackID: String)] = []
    private(set) var removeCalls: [String] = []

    func seed(_ room: CollectionRoom) { rooms[room.id] = room }

    func assignCollectionRoomMusic(accessToken: String, collectionRoomID: String, musicTrackID: String) async throws -> CollectionRoom {
        assignCalls.append((collectionRoomID, musicTrackID))
        guard let room = rooms[collectionRoomID] else {
            throw CollectionAPIError(statusCode: 404, code: nil, message: "not found")
        }
        guard knownTracks.contains(musicTrackID) else {
            throw CollectionAPIError(statusCode: 400, code: "unknown_music_track", message: "music track is not in the catalog")
        }
        let updated = CollectionRoom(
            id: room.id, name: room.name, categoryID: room.categoryID, designID: room.designID,
            currentTier: room.currentTier, items: room.items, musicTrackID: musicTrackID
        )
        rooms[collectionRoomID] = updated
        return updated
    }

    func removeCollectionRoomMusic(accessToken: String, collectionRoomID: String) async throws -> CollectionRoom {
        removeCalls.append(collectionRoomID)
        guard let room = rooms[collectionRoomID] else {
            throw CollectionAPIError(statusCode: 404, code: nil, message: "not found")
        }
        let updated = CollectionRoom(
            id: room.id, name: room.name, categoryID: room.categoryID, designID: room.designID,
            currentTier: room.currentTier, items: room.items, musicTrackID: nil
        )
        rooms[collectionRoomID] = updated
        return updated
    }
}

@MainActor
private final class SpyPlayer: RoomMusicPlaying {
    private(set) var started: [String] = []
    private(set) var stops = 0
    private(set) var muteCalls: [Bool] = []
    var currentTrackID: String?

    func start(url: URL, trackID: String) {
        started.append(trackID)
        currentTrackID = trackID
    }

    func stop() {
        stops += 1
        currentTrackID = nil
    }

    func setMuted(_ muted: Bool) { muteCalls.append(muted) }
}

private struct StubCatalog: MusicCatalogServicing {
    var tracks: [MusicTrack] = [
        MusicTrack(id: "track_dev_a", displayName: "DEV TEST TONE A", attribution: "Muse (test audio)", licensing: .devTest, durationSeconds: 12),
        MusicTrack(id: "track_dev_b", displayName: "DEV TEST TONE B", attribution: "Muse (test audio)", licensing: .devTest, durationSeconds: 8),
    ]
    final class Counter: @unchecked Sendable { var requested: [String] = [] }
    var counter = Counter()

    func fetchMusicTracks(accessToken: String) async throws -> [MusicTrack] { tracks }

    func musicAudioURL(accessToken: String, trackID: String) async throws -> MusicAudioURL {
        counter.requested.append(trackID)
        guard tracks.contains(where: { $0.id == trackID }) else {
            throw IdentityAPIClientError.server(statusCode: 404, message: "not found")
        }
        return MusicAudioURL(url: URL(string: "https://cdn.example/\(trackID).mp3")!, expiresAt: Date(timeIntervalSinceNow: 300))
    }
}

// MARK: - The owner's assignment, through the shared screen

@MainActor
final class CollectionMusicAssignmentTests: XCTestCase {
    private func makeCollectionViewModel(
        service: FakeCollectionRoomMusicService, room: CollectionRoom
    ) -> RoomMusicSelectionViewModel {
        RoomMusicSelectionViewModel(
            assignedTrackID: room.musicTrackID,
            assignment: CollectionRoomMusicAssignment(service: service, accessToken: "token", collectionRoomID: room.id),
            musicCatalog: StubCatalog(),
            accessToken: "token"
        )
    }

    func test_ownerAssignsAValidTrack_throughTheViewModel() async {
        let service = FakeCollectionRoomMusicService()
        let room = CollectionRoom(id: "cr-1", name: "Watches", categoryID: "category_watches")
        service.seed(room)
        let viewModel = makeCollectionViewModel(service: service, room: room)
        await viewModel.load()

        await viewModel.assign(trackID: "track_dev_a")

        XCTAssertEqual(viewModel.assignedTrackID, "track_dev_a")
        XCTAssertEqual(viewModel.state, .loaded(tracks: StubCatalog().tracks, assignedTrackID: "track_dev_a"))
        XCTAssertEqual(service.assignCalls.map(\.roomID), ["cr-1"])
        XCTAssertEqual(service.rooms["cr-1"]?.musicTrackID, "track_dev_a")
        XCTAssertEqual(service.rooms["cr-1"]?.name, "Watches", "a music change touches nothing else")
    }

    func test_ownerChangesAndRemovesTheTrack() async {
        let service = FakeCollectionRoomMusicService()
        let room = CollectionRoom(id: "cr-1", name: "Watches", musicTrackID: "track_dev_a")
        service.seed(room)
        let viewModel = makeCollectionViewModel(service: service, room: room)
        await viewModel.load()
        XCTAssertEqual(viewModel.assignedTrackID, "track_dev_a", "the screen opens on the server's assignment")

        await viewModel.assign(trackID: "track_dev_b")
        XCTAssertEqual(viewModel.assignedTrackID, "track_dev_b")

        await viewModel.removeMusic()
        XCTAssertNil(viewModel.assignedTrackID)
        XCTAssertEqual(viewModel.state, .loaded(tracks: StubCatalog().tracks, assignedTrackID: nil))
        XCTAssertEqual(service.removeCalls, ["cr-1"])
        XCTAssertNil(service.rooms["cr-1"]?.musicTrackID)
    }

    func test_anInvalidTrack_isRejected_andNothingChanges() async {
        let service = FakeCollectionRoomMusicService()
        let room = CollectionRoom(id: "cr-1", name: "Watches", musicTrackID: "track_dev_a")
        service.seed(room)
        let viewModel = makeCollectionViewModel(service: service, room: room)
        await viewModel.load()

        await viewModel.assign(trackID: "track_nope")

        guard case .failed(let message, let tracks) = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
        XCTAssertEqual(message, "Couldn't set that track. Your Room's music is unchanged.")
        XCTAssertFalse(tracks.isEmpty, "a failed assignment must not empty the music library")
        XCTAssertEqual(viewModel.assignedTrackID, "track_dev_a", "the view model converges on the server, which refused")
        XCTAssertEqual(service.rooms["cr-1"]?.musicTrackID, "track_dev_a")
    }

    func test_collectionAndMuseumAssignments_areIndependent() async {
        let collectionService = FakeCollectionRoomMusicService()
        collectionService.seed(CollectionRoom(id: "cr-1", name: "Watches"))
        let museumService = FakeMuseumService()
        museumService.musicCatalog = StubCatalog().tracks

        let collectionViewModel = RoomMusicSelectionViewModel(
            assignedTrackID: nil,
            assignment: CollectionRoomMusicAssignment(service: collectionService, accessToken: "token", collectionRoomID: "cr-1"),
            musicCatalog: StubCatalog(), accessToken: "token"
        )
        let museumViewModel = RoomMusicSelectionViewModel(
            assignedTrackID: nil,
            assignment: MuseumRoomMusicAssignment(museumService: museumService, accessToken: "token", roomID: "room-1"),
            musicCatalog: StubCatalog(), accessToken: "token"
        )
        await collectionViewModel.load()
        await museumViewModel.load()

        await collectionViewModel.assign(trackID: "track_dev_a")
        await museumViewModel.assign(trackID: "track_dev_b")

        XCTAssertEqual(collectionViewModel.assignedTrackID, "track_dev_a")
        XCTAssertEqual(museumViewModel.assignedTrackID, "track_dev_b")
        XCTAssertEqual(collectionService.assignCalls.map(\.trackID), ["track_dev_a"], "the Collection service saw only the Collection assignment")
        XCTAssertEqual(museumService.assignedMusic.map(\.trackID), ["track_dev_b"], "the Museum service saw only the Museum assignment")
        XCTAssertTrue(museumService.removedMusic.isEmpty)
        XCTAssertTrue(collectionService.removeCalls.isEmpty)

        await museumViewModel.removeMusic()
        XCTAssertNil(museumViewModel.assignedTrackID)
        XCTAssertEqual(museumService.removedMusic, ["room-1"])
        XCTAssertEqual(collectionService.rooms["cr-1"]?.musicTrackID, "track_dev_a", "removing a Museum Room's music left the Collection Room's alone")
    }

    func test_ownerScreen_playsThroughTheSession_andTransitionsOnAssign() async {
        let service = FakeCollectionRoomMusicService()
        let room = CollectionRoom(id: "cr-1", name: "Watches", musicTrackID: "track_dev_a")
        service.seed(room)
        let player = SpyPlayer()
        let catalog = StubCatalog()
        let session = RoomMusicSession(trackID: room.musicTrackID, catalog: catalog, player: player, accessToken: "token")
        let viewModel = makeCollectionViewModel(service: service, room: room)
        let controller = RoomMusicSelectionViewController(viewModel: viewModel, musicSession: session) { _ in }

        controller.loadViewIfNeeded()
        XCTAssertTrue(controller.testMusicSession === session, "the screen hosts the engine it was given — no session of its own")
        controller.viewWillAppear(false)
        await controller.testAwaitMusic()
        XCTAssertEqual(player.started, ["track_dev_a"], "plays on entry")
        XCTAssertEqual(catalog.counter.requested, ["track_dev_a"], "one audio URL resolved, by the session")
        XCTAssertNotNil(controller.testMusicToggleItem, "the local mute is offered while a track is assigned")

        await viewModel.load()
        await viewModel.assign(trackID: "track_dev_b")
        await controller.testAwaitMusic()
        XCTAssertEqual(player.started, ["track_dev_a", "track_dev_b"])
        XCTAssertGreaterThanOrEqual(player.stops, 1, "the previous audio stops before the next starts")

        await viewModel.removeMusic()
        await controller.testAwaitMusic()
        XCTAssertNil(controller.testMusicToggleItem, "nothing to mute once there is no track")
        controller.viewWillDisappear(false)
        XCTAssertNil(player.currentTrackID, "leaving never leaves sound behind")
    }

    func test_museumScreen_hostsNoSession() {
        let museumService = FakeMuseumService()
        let viewModel = RoomMusicSelectionViewModel(
            assignedTrackID: "track_dev_a",
            assignment: MuseumRoomMusicAssignment(museumService: museumService, accessToken: "token", roomID: "room-1"),
            musicCatalog: StubCatalog(), accessToken: "token"
        )
        let controller = RoomMusicSelectionViewController(viewModel: viewModel) { _ in }
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        XCTAssertNil(controller.testMusicSession)
        XCTAssertNil(controller.testMusicToggleItem)
    }
}

// MARK: - The visitor: the same engine, behind the same gate

@MainActor
final class SharedCollectionRoomMusicTests: XCTestCase {
    private func content(musicTrackID: String?) -> SharedCollectionRoomContent {
        SharedCollectionRoomContent(
            collectionRoomID: "cr-1", name: "Shared Watches", categoryID: "category_watches", designID: nil,
            currentTier: .base, items: [], musicTrackID: musicTrackID
        )
    }

    func test_visitorWithATrack_playsThroughTheSession_andCanMuteLocally() async {
        let player = SpyPlayer()
        let catalog = StubCatalog()
        let controller = SharedCollectionRoomViewController(
            content: content(musicTrackID: "track_dev_a"), musicCatalog: catalog, musicPlayer: player, accessToken: "token"
        )
        controller.loadViewIfNeeded()
        let session = controller.testMusicSession
        XCTAssertNotNil(session)
        XCTAssertTrue(type(of: session!) == RoomMusicSession.self, "the engine, not a Collection-specific one")

        controller.viewWillAppear(false)
        await controller.testAwaitMusic()
        XCTAssertEqual(player.started, ["track_dev_a"], "plays on entry")
        XCTAssertEqual(catalog.counter.requested, ["track_dev_a"])

        let toggle = controller.testMusicToggleItem
        XCTAssertNotNil(toggle)
        _ = toggle?.target?.perform(toggle!.action!, with: toggle)
        XCTAssertEqual(player.muteCalls.last, true)
        XCTAssertTrue(session!.state.isMutedLocally)
        XCTAssertEqual(player.started.count, 1, "muting does not reload")

        controller.viewWillDisappear(false)
        XCTAssertNil(player.currentTrackID, "leaving stops the audio")
    }

    func test_visitorWithoutATrack_hasNoSessionNoRequestNoControl() async {
        let player = SpyPlayer()
        let catalog = StubCatalog()
        let controller = SharedCollectionRoomViewController(
            content: content(musicTrackID: nil), musicCatalog: catalog, musicPlayer: player, accessToken: "token"
        )
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        await controller.testAwaitMusic()

        XCTAssertNil(controller.testMusicSession)
        XCTAssertNil(controller.testMusicToggleItem)
        XCTAssertTrue(player.started.isEmpty)
        XCTAssertTrue(catalog.counter.requested.isEmpty, "no audio URL is ever requested for a Room the visitor was not told has music")
    }

    func test_twoVisitors_ownIndependentSessions() async {
        let first = SharedCollectionRoomViewController(content: content(musicTrackID: "track_dev_a"), musicCatalog: StubCatalog(), musicPlayer: SpyPlayer(), accessToken: "t1")
        let second = SharedCollectionRoomViewController(content: content(musicTrackID: "track_dev_a"), musicCatalog: StubCatalog(), musicPlayer: SpyPlayer(), accessToken: "t2")
        first.loadViewIfNeeded()
        second.loadViewIfNeeded()

        first.testMusicSession?.toggleMute()

        XCTAssertTrue(first.testMusicSession!.state.isMutedLocally)
        XCTAssertFalse(second.testMusicSession!.state.isMutedLocally, "one viewer's toggle cannot reach another")
        XCTAssertFalse(first.testMusicSession === second.testMusicSession)
    }

    func test_visitorScreenHoldsOnlyReadOnlyAndLocalOnlyMusicTypes() {
        let anyCatalog: any MusicCatalogServicing = StubCatalog()
        let anyPlayer: any RoomMusicPlaying = SpyPlayer()
        XCTAssertFalse(anyCatalog is CollectionRoomMusicServicing)
        XCTAssertFalse(anyPlayer is CollectionRoomMusicServicing)
        XCTAssertFalse(anyCatalog is MusicAssigning)
    }
}
