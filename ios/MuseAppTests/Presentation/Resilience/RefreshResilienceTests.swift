import XCTest
@testable import MuseApp

@MainActor
final class RefreshResilienceTests: XCTestCase {

    // MARK: - Rooms

    func test_roomList_failedRefreshKeepsTheRoomsAndSaysTheyMayBeStale() async {
        let service = FakeMuseumService()
        service.roomsResult = .success([room(id: "r1", name: "Ground Floor")])
        let viewModel = RoomListViewModel(museumService: service, accessToken: "t")

        await viewModel.load()
        XCTAssertEqual(viewModel.state, .loaded(rooms: [room(id: "r1", name: "Ground Floor")]))
        XCTAssertNil(viewModel.refreshFailureNotice)

        service.roomsResult = .failure(URLError(.notConnectedToInternet))
        await viewModel.load()

        guard case .loaded(let rooms) = viewModel.state else {
            return XCTFail("a failed refresh must not replace the Rooms with an error: \(viewModel.state)")
        }
        XCTAssertEqual(rooms.map(\.id), ["r1"])
        let notice = try? XCTUnwrap(viewModel.refreshFailureNotice)
        XCTAssertTrue((notice ?? "").lowercased().contains("offline"), notice ?? "nil")
        XCTAssertTrue((notice ?? "").contains("last loaded"), notice ?? "nil")
    }

    func test_roomList_firstLoadFailureIsAFailureState() async {
        let service = FakeMuseumService()
        service.roomsResult = .failure(URLError(.notConnectedToInternet))
        let viewModel = RoomListViewModel(museumService: service, accessToken: "t")

        await viewModel.load()

        guard case .failed(let message) = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
        XCTAssertTrue(message.lowercased().contains("offline"), message)
        XCTAssertNil(viewModel.refreshFailureNotice, "the notice is for content that survived; there is none")
    }

    func test_roomList_successfulRefreshClearsTheNotice() async {
        let service = FakeMuseumService()
        service.roomsResult = .success([room(id: "r1", name: "A")])
        let viewModel = RoomListViewModel(museumService: service, accessToken: "t")
        await viewModel.load()
        service.roomsResult = .failure(URLError(.timedOut))
        await viewModel.load()
        XCTAssertNotNil(viewModel.refreshFailureNotice)

        service.roomsResult = .success([room(id: "r1", name: "A"), room(id: "r2", name: "B")])
        await viewModel.load()

        XCTAssertNil(viewModel.refreshFailureNotice)
        XCTAssertEqual(viewModel.state, .loaded(rooms: [room(id: "r1", name: "A"), room(id: "r2", name: "B")]))
    }

    // MARK: - Museum

    func test_museumEntry_failedRefreshKeepsTheMuseum() async {
        let service = FakeMuseumService()
        let museum = Museum(id: "m1", styleID: "style_a", privacy: .private)
        service.fetchResult = .success(museum)
        let viewModel = MuseumEntryViewModel(museumService: service, accessToken: "t")
        await viewModel.load()
        XCTAssertEqual(viewModel.state, .hasMuseum(museum))

        service.fetchResult = .failure(URLError(.notConnectedToInternet))
        await viewModel.load()

        XCTAssertEqual(viewModel.state, .hasMuseum(museum), "the Museum must survive a failed refresh")
        XCTAssertNotNil(viewModel.refreshFailureNotice)
        XCTAssertFalse(viewModel.canOfferCreation, "a stale Museum must never make creation look available")
    }

    func test_museumEntry_serverAnswerOverridesPreviouslyLoadedContent() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(Museum(id: "m1", styleID: "style_a", privacy: .private))
        let viewModel = MuseumEntryViewModel(museumService: service, accessToken: "t")
        await viewModel.load()

        service.fetchResult = .failure(IdentityAPIClientError.server(statusCode: 404, message: nil))
        await viewModel.load()

        XCTAssertEqual(viewModel.state, .needsCreation)
        XCTAssertNil(viewModel.refreshFailureNotice)
    }

    // MARK: - Profile

    func test_profile_failedRefreshKeepsTheProfile() async {
        let service = FakeProfileService()
        service.result = .success(Profile(displayName: "Ada", avatarID: "avatar_2"))
        let viewModel = ProfileViewModel(profileService: service, accessToken: "t")
        await viewModel.load()

        service.result = .failure(URLError(.timedOut))
        await viewModel.load()

        XCTAssertEqual(viewModel.state, .loaded(Profile(displayName: "Ada", avatarID: "avatar_2")))
        XCTAssertNotNil(viewModel.refreshFailureNotice)
    }

    func test_profile_saveFailureIsAMutationFailureNotAStaleNotice() async {
        let service = FakeProfileService()
        service.result = .success(Profile(displayName: "Ada", avatarID: "avatar_2"))
        let viewModel = ProfileViewModel(profileService: service, accessToken: "t")
        await viewModel.load()

        service.result = .failure(URLError(.notConnectedToInternet))
        await viewModel.save(displayName: "Grace")

        guard case .failed(let message) = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
        XCTAssertTrue(message.contains("wasn't saved"), message)
    }

    // MARK: - Privacy: the deliberate exception

    func test_privacySettings_deliberatelyDoesNotShowStalePrivacy() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(Museum(id: "m1", styleID: "s", privacy: .public))
        service.roomsResult = .success([room(id: "r1", name: "A")])
        let viewModel = PrivacySettingsViewModel(museumService: service, accessToken: "t")
        await viewModel.load()
        guard case .loaded = viewModel.state else { return XCTFail("expected a first load") }

        service.fetchResult = .failure(URLError(.notConnectedToInternet))
        await viewModel.load()

        guard case .failed(let message) = viewModel.state else {
            return XCTFail("privacy must not be served from memory on a failed refresh: \(viewModel.state)")
        }
        XCTAssertTrue(message.lowercased().contains("offline"), message)
    }

    // MARK: - Staleness

    func test_anOlderLoadCannotOverwriteANewerOne() async {
        let service = GatedMuseumService()
        service.rooms = [room(id: "r1", name: "First")]
        let viewModel = RoomListViewModel(museumService: service, accessToken: "t")

        let gateOpened = expectation(description: "load A reached the gate")
        service.onEnter = { gateOpened.fulfill() }
        service.holdNextCall = true
        let loadA = Task { await viewModel.load() }
        await fulfillment(of: [gateOpened], timeout: 2)

        service.holdNextCall = false
        service.onEnter = nil
        service.rooms = [room(id: "r2", name: "Second")]
        await viewModel.load()
        XCTAssertEqual(viewModel.stateRoomIDs, ["r2"])

        service.failHeldCall = true
        service.releaseHeldCall()
        await loadA.value

        XCTAssertEqual(viewModel.stateRoomIDs, ["r2"],
                       "a superseded load's failure must not replace the newer content")
        XCTAssertNil(viewModel.refreshFailureNotice,
                     "nor may it mark the newer content as stale")
    }

    // MARK: - Support

    private func room(id: String, name: String) -> Room {
        Room(id: id, name: name, variantID: "variant_a", privacy: .private)
    }
}

private extension RoomListViewModel {
    var stateRoomIDs: [String] {
        if case .loaded(let rooms) = state { return rooms.map(\.id) }
        return []
    }
}

@MainActor
final class GatedMuseumService: MuseumServicing {
    var rooms: [Room] = []
    var holdNextCall = false
    var failHeldCall = false
    var onEnter: (() -> Void)?
    private var continuation: CheckedContinuation<Void, Never>?

    func releaseHeldCall() {
        continuation?.resume()
        continuation = nil
    }

    func listRooms(accessToken: String) async throws -> [Room] {
        if holdNextCall {
            holdNextCall = false
            onEnter?()
            await withCheckedContinuation { continuation = $0 }
            if failHeldCall { throw URLError(.timedOut) }
        }
        return rooms
    }

    func createMuseum(accessToken: String, styleID: String) async throws -> Museum { throw URLError(.unknown) }
    func fetchMuseum(accessToken: String) async throws -> Museum { throw URLError(.unknown) }
    func changeStyle(accessToken: String, styleID: String) async throws -> Museum { throw URLError(.unknown) }
    func changePrivacy(accessToken: String, privacy: MusePrivacy) async throws -> Museum { throw URLError(.unknown) }
    func createRoom(accessToken: String, name: String, variantID: String) async throws -> Room { throw URLError(.unknown) }
    func fetchRoom(accessToken: String, roomID: String) async throws -> Room { throw URLError(.unknown) }
    func updateRoom(accessToken: String, roomID: String, patch: RoomPatch) async throws -> Room { throw URLError(.unknown) }
    func assignRoomMusic(accessToken: String, roomID: String, musicTrackID: String) async throws -> Room { throw URLError(.unknown) }
    func removeRoomMusic(accessToken: String, roomID: String) async throws -> Room { throw URLError(.unknown) }
    func addSculpture(accessToken: String, roomID: String, catalogID: String) async throws -> [SculptureInstance] { throw URLError(.unknown) }
    func removeSculpture(accessToken: String, roomID: String, slotIndex: Int) async throws -> [SculptureInstance] { throw URLError(.unknown) }
    func fetchStyles(accessToken: String) async throws -> [MuseumStyle] { throw URLError(.unknown) }
    func fetchVariants(accessToken: String, styleID: String) async throws -> [RoomVariant] { throw URLError(.unknown) }
    func fetchSculptures(accessToken: String) async throws -> [SculptureCatalogEntry] { throw URLError(.unknown) }
}
