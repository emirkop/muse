import UIKit
import XCTest
@testable import MuseApp

// MARK: - The local state value

final class RoomMusicStateTests: XCTestCase {
    func test_noTrack_playsNothing_andOffersNoToggle() {
        let state = RoomMusicState(trackID: nil)

        XCTAssertFalse(state.hasTrack)
        XCTAssertFalse(state.shouldPlay)
        XCTAssertFalse(state.offersToggle, "a control over a silent Room would imply music that does not exist")
    }

    func test_withATrack_playsUntilMutedLocally() {
        let playing = RoomMusicState(trackID: "track_a")
        XCTAssertTrue(playing.shouldPlay)
        XCTAssertTrue(playing.offersToggle)
        XCTAssertEqual(playing.toggleTitle, "Mute Music")

        let muted = playing.togglingMute()
        XCTAssertTrue(muted.isMutedLocally)
        XCTAssertFalse(muted.shouldPlay)
        XCTAssertEqual(muted.toggleTitle, "Unmute Music")
        XCTAssertEqual(muted.trackID, "track_a", "muting must not forget what is assigned")

        XCTAssertFalse(muted.togglingMute().isMutedLocally)
    }

    func test_stateIsAValue_soOneViewersToggleCannotReachAnother() {
        let visitorA = RoomMusicState(trackID: "track_a")
        let visitorB = visitorA

        let mutedA = visitorA.togglingMute()

        XCTAssertTrue(mutedA.isMutedLocally)
        XCTAssertFalse(visitorB.isMutedLocally, "the other viewer is unaffected")
        XCTAssertTrue(visitorB.shouldPlay)
    }
}

// MARK: - Playback session

@MainActor
private final class SpyMusicPlayer: RoomMusicPlaying {
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

private struct StubMusicCatalog: MusicCatalogServicing {
    var tracks: [MusicTrack] = []
    var urls: [String: MusicAudioURL] = [:]
    final class Counter: @unchecked Sendable { var value = 0; var requested: [String] = [] }
    var counter = Counter()

    func fetchMusicTracks(accessToken: String) async throws -> [MusicTrack] { tracks }

    func musicAudioURL(accessToken: String, trackID: String) async throws -> MusicAudioURL {
        counter.value += 1
        counter.requested.append(trackID)
        guard let url = urls[trackID] else {
            throw IdentityAPIClientError.server(statusCode: 404, message: "not found")
        }
        return url
    }
}

private func audioURL(_ id: String) -> MusicAudioURL {
    MusicAudioURL(url: URL(string: "https://cdn.example/\(id).mp3")!, expiresAt: Date(timeIntervalSinceNow: 300))
}

@MainActor
final class RoomMusicSessionTests: XCTestCase {
    private func makeSession(
        trackID: String?,
        catalog: StubMusicCatalog = StubMusicCatalog(urls: ["track_a": audioURL("track_a"), "track_b": audioURL("track_b")])
    ) -> (RoomMusicSession, SpyMusicPlayer, StubMusicCatalog) {
        let player = SpyMusicPlayer()
        let session = RoomMusicSession(trackID: trackID, catalog: catalog, player: player, accessToken: "token")
        return (session, player, catalog)
    }

    func test_enteringARoomWithATrack_startsPlayback() async {
        let (session, player, catalog) = makeSession(trackID: "track_a")

        await session.enterRoom()

        XCTAssertEqual(player.started, ["track_a"])
        XCTAssertEqual(catalog.counter.requested, ["track_a"])
        XCTAssertTrue(session.state.shouldPlay)
        XCTAssertNil(session.lastFailure)
    }

    func test_enteringARoomWithoutATrack_resolvesNothing() async {
        let (session, player, catalog) = makeSession(trackID: nil)

        await session.enterRoom()

        XCTAssertTrue(player.started.isEmpty)
        XCTAssertEqual(catalog.counter.value, 0)
    }

    func test_theLocalToggleMutesAndUnmutesWithoutReloading() async {
        let (session, player, catalog) = makeSession(trackID: "track_a")
        await session.enterRoom()

        session.toggleMute()
        XCTAssertEqual(player.muteCalls.last, true)
        XCTAssertTrue(session.state.isMutedLocally)
        XCTAssertFalse(session.state.shouldPlay)

        session.toggleMute()
        XCTAssertEqual(player.muteCalls.last, false)
        XCTAssertFalse(session.state.isMutedLocally)
        XCTAssertEqual(player.started, ["track_a"], "unmuting must resume, not reload")
        XCTAssertEqual(catalog.counter.value, 1, "and must not re-resolve the URL")
    }

    func test_mutedOnEntry_fetchesNothingUntilUnmuted() async {
        let (session, player, catalog) = makeSession(trackID: "track_a")
        session.toggleMute()

        await session.enterRoom()
        XCTAssertEqual(catalog.counter.value, 0, "a muted viewer downloads no audio")
        XCTAssertTrue(player.started.isEmpty)

        session.toggleMute()
        for _ in 0..<40 where player.started.isEmpty { await Task.yield() }
        XCTAssertEqual(player.started, ["track_a"], "unmuting loads it")
    }

    func test_leavingTheRoom_stopsPlayback() async {
        let (session, player, _) = makeSession(trackID: "track_a")
        await session.enterRoom()

        session.leaveRoom()

        XCTAssertGreaterThanOrEqual(player.stops, 1)
        XCTAssertNil(player.currentTrackID)
    }

    func test_changingTheRoomsTrack_transitionsSafely() async {
        let (session, player, _) = makeSession(trackID: "track_a")
        await session.enterRoom()

        await session.roomTrackChanged(to: "track_b")

        XCTAssertEqual(player.started, ["track_a", "track_b"])
        XCTAssertGreaterThanOrEqual(player.stops, 1, "the previous track must be stopped first")
        XCTAssertEqual(session.state.trackID, "track_b")
    }

    func test_removingTheRoomsTrack_stopsAndStaysSilent() async {
        let (session, player, _) = makeSession(trackID: "track_a")
        await session.enterRoom()

        await session.roomTrackChanged(to: nil)

        XCTAssertGreaterThanOrEqual(player.stops, 1)
        XCTAssertEqual(player.started, ["track_a"], "nothing new may start")
        XCTAssertFalse(session.state.hasTrack)
        XCTAssertFalse(session.state.shouldPlay)
    }

    func test_aLocalMuteSurvivesATrackChange() async {
        let (session, player, _) = makeSession(trackID: "track_a")
        await session.enterRoom()
        session.toggleMute()

        await session.roomTrackChanged(to: "track_b")

        XCTAssertTrue(session.state.isMutedLocally)
        XCTAssertFalse(session.state.shouldPlay)
        XCTAssertEqual(player.started, ["track_a"], "a muted viewer must not start the new track")
    }

    func test_whenTheDeploymentWillNotServeTheTrack_theRoomIsSilentNotBroken() async {
        let catalog = StubMusicCatalog(urls: [:])
        let (session, player, _) = makeSession(trackID: "track_missing", catalog: catalog)

        await session.enterRoom()

        XCTAssertEqual(session.lastFailure, .notFound)
        XCTAssertTrue(player.started.isEmpty)
        XCTAssertTrue(session.state.hasTrack, "the Room still has a track assigned; it just cannot be heard")
    }
}

// MARK: - Two viewers, one Room (property, now with real state)

@MainActor
final class ConcurrentRoomMusicIsolationTests: XCTestCase {
    func test_twoViewersInTheSameRoom_muteIndependently() async {
        let catalog = StubMusicCatalog(urls: ["track_a": audioURL("track_a")])
        let playerA = SpyMusicPlayer()
        let playerB = SpyMusicPlayer()
        let visitorA = RoomMusicSession(trackID: "track_a", catalog: catalog, player: playerA, accessToken: "token-a")
        let visitorB = RoomMusicSession(trackID: "track_a", catalog: catalog, player: playerB, accessToken: "token-b")
        await visitorA.enterRoom()
        await visitorB.enterRoom()

        visitorA.toggleMute()

        XCTAssertTrue(visitorA.state.isMutedLocally)
        XCTAssertFalse(visitorB.state.isMutedLocally, "one viewer's mute must not reach another")
        XCTAssertEqual(playerA.muteCalls.last, true)
        XCTAssertTrue(playerB.muteCalls.allSatisfy { $0 == false }, "the other viewer's player is never muted")
        XCTAssertTrue(visitorB.state.shouldPlay)
    }

    func test_oneViewerLeaving_doesNotSilenceTheOther() async {
        let catalog = StubMusicCatalog(urls: ["track_a": audioURL("track_a")])
        let playerA = SpyMusicPlayer()
        let playerB = SpyMusicPlayer()
        let a = RoomMusicSession(trackID: "track_a", catalog: catalog, player: playerA, accessToken: "t")
        let b = RoomMusicSession(trackID: "track_a", catalog: catalog, player: playerB, accessToken: "t")
        await a.enterRoom()
        await b.enterRoom()

        a.leaveRoom()

        XCTAssertNil(playerA.currentTrackID)
        XCTAssertEqual(playerB.currentTrackID, "track_a", "the remaining viewer still hears the Room")
        XCTAssertEqual(playerB.stops, 0)
    }

    func test_eachViewerResolvesItsOwnAudioURL() async {
        let catalog = StubMusicCatalog(urls: ["track_a": audioURL("track_a")])
        let a = RoomMusicSession(trackID: "track_a", catalog: catalog, player: SpyMusicPlayer(), accessToken: "token-a")
        let b = RoomMusicSession(trackID: "track_a", catalog: catalog, player: SpyMusicPlayer(), accessToken: "token-b")

        await a.enterRoom()
        await b.enterRoom()

        XCTAssertEqual(catalog.counter.value, 2, "two viewers, two resolutions")
    }
}

// MARK: - Runtime: who gets which affordance

@MainActor
final class RoomMusicRuntimeAffordanceTests: XCTestCase {
    private func controller(role: RoomViewerRole, musicTrackID: String?, withOwnerServices: Bool) -> RealityKitSceneViewController {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 2)!
        let room = Room(
            id: fixture.room.id, name: fixture.room.name, variantID: fixture.room.variantID,
            privacy: fixture.room.privacy, musicTrackID: musicTrackID,
            photoSlots: fixture.room.photoSlots, sculptures: fixture.room.sculptures
        )
        let content = RoomRuntimeContent(
            roomID: fixture.roomID, accessToken: fixture.accessToken, geometry: fixture.geometry,
            viewerRole: role, room: room, slotTable: fixture.slotTable,
            placements: fixture.placements, textures: fixture.textures,
            photoService: withOwnerServices ? fixture.photoService : nil,
            roomService: withOwnerServices ? fixture.roomService : nil,
            catalogService: nil,
            musicCatalog: StubMusicCatalog(urls: ["track_a": audioURL("track_a")]),
            musicPlayer: SpyMusicPlayer()
        )
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        return controller
    }

    private func barIdentifiers(_ controller: UIViewController) -> [String] {
        var items = controller.navigationItem.leftBarButtonItems ?? []
        if let right = controller.navigationItem.rightBarButtonItem { items.append(right) }
        items.append(contentsOf: controller.navigationItem.rightBarButtonItems ?? [])
        return items.compactMap(\.accessibilityIdentifier)
    }

    func test_visitor_getsTheLocalMuteButNoAssignment() {
        let visitor = controller(role: .visitor, musicTrackID: "track_a", withOwnerServices: true)

        XCTAssertNotNil(visitor.musicSession, "a visitor hears the Room's music")
        XCTAssertTrue(barIdentifiers(visitor).contains("room-music-toggle"))
        XCTAssertFalse(barIdentifiers(visitor).contains("room-music-assign"), "only the owner assigns music")
        XCTAssertEqual(visitor.content?.supportsMusicAssignment, false)
        XCTAssertNil(visitor.editMode)
    }

    func test_owner_getsBothTheMuteAndTheAssignmentAction() {
        let owner = controller(role: .owner, musicTrackID: "track_a", withOwnerServices: true)

        XCTAssertNotNil(owner.musicSession)
        XCTAssertTrue(barIdentifiers(owner).contains("room-music-toggle"))
        XCTAssertEqual(owner.content?.supportsMusicAssignment, true)
        XCTAssertFalse(barIdentifiers(owner).contains("room-music-assign"), "not before entering Edit Mode")
        let editToggle = (owner.navigationItem.rightBarButtonItems ?? [])
            .first { $0.accessibilityIdentifier == "room-edit-mode-toggle" }
            ?? owner.navigationItem.rightBarButtonItem
        _ = editToggle?.target?.perform(editToggle?.action, with: editToggle)

        XCTAssertEqual(owner.editMode?.isEditing, true)
        XCTAssertTrue(barIdentifiers(owner).contains("room-music-assign"), "present while editing")
    }

    func test_aRoomWithNoMusic_hasNoSessionAndNoControl() {
        let controller = controller(role: .owner, musicTrackID: nil, withOwnerServices: true)

        XCTAssertNil(controller.musicSession)
        XCTAssertFalse(barIdentifiers(controller).contains("room-music-toggle"))
        XCTAssertEqual(controller.content?.supportsMusicPlayback, false)
    }

    func test_twoControllers_ownDistinctMusicSessions() {
        let a = controller(role: .visitor, musicTrackID: "track_a", withOwnerServices: true)
        let b = controller(role: .visitor, musicTrackID: "track_a", withOwnerServices: true)

        XCTAssertNotNil(a.musicSession)
        XCTAssertNotNil(b.musicSession)
        XCTAssertFalse(a.musicSession === b.musicSession)
    }
}
