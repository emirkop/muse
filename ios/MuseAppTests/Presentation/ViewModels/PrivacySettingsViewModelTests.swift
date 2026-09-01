import XCTest
@testable import MuseApp

@MainActor
final class PrivacySettingsViewModelTests: XCTestCase {

    private func makeService(
        museumPrivacy: MusePrivacy = .private,
        rooms: [Room] = [
            Room(id: "r1", name: "The Long Hall", variantID: "v1", privacy: .public,
                 photoSlots: [PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a1", caption: "kept")]),
            Room(id: "r2", name: "Study", variantID: "v2", privacy: .private)
        ]
    ) -> FakeMuseumService {
        let service = FakeMuseumService()
        service.fetchResult = .success(Museum(id: "m1", styleID: "style_modern", privacy: museumPrivacy))
        service.roomsResult = .success(rooms)
        return service
    }

    private func makeViewModel(_ service: FakeMuseumService) -> PrivacySettingsViewModel {
        PrivacySettingsViewModel(museumService: service, accessToken: "token")
    }

    private func content(_ viewModel: PrivacySettingsViewModel) throws -> PrivacySettingsViewModel.Content {
        guard case .loaded(let content) = viewModel.state else {
            throw XCTSkip("expected .loaded, got \(viewModel.state)")
        }
        return content
    }

    // MARK: - Showing the current state

    func test_load_showsBothLevels_withEffectiveVisibilityPerRoom() async throws {
        let viewModel = makeViewModel(makeService(museumPrivacy: .private))

        await viewModel.load()

        let content = try content(viewModel)
        XCTAssertEqual(content.museumPrivacy, .private)
        XCTAssertEqual(content.museumStateLabel, "Private")
        XCTAssertEqual(content.rooms.count, 2)
        XCTAssertEqual(content.rooms[0].stateLabel, "Public")
        XCTAssertEqual(content.rooms[0].visibility, .hiddenByMuseum)
        XCTAssertEqual(content.rooms[0].visibilityDescription,
                       "Hidden from visitors — your Museum is Private.")
        XCTAssertEqual(content.rooms[1].visibility, .hiddenByMuseum)
    }

    func test_load_whenMuseumIsPublic_distinguishesRoomLevelHiding() async throws {
        let viewModel = makeViewModel(makeService(museumPrivacy: .public))

        await viewModel.load()

        let content = try content(viewModel)
        XCTAssertEqual(content.rooms[0].visibility, .visible)
        XCTAssertEqual(content.rooms[1].visibility, .hiddenByRoom)
        XCTAssertEqual(content.museumDescription,
                       "Your Museum is Public — visitors can enter and see its Public Rooms.")
    }

    func test_privateMuseumDescription_statesThatRoomSettingsDoNotMatter() async throws {
        let viewModel = makeViewModel(makeService(museumPrivacy: .private))
        await viewModel.load()

        XCTAssertEqual(try content(viewModel).museumDescription,
                       "Your Museum is Private — no one can enter, regardless of individual Room settings.")
    }

    func test_load_withNoRooms_isANormalState() async throws {
        let viewModel = makeViewModel(makeService(rooms: []))

        await viewModel.load()

        XCTAssertTrue(try content(viewModel).rooms.isEmpty)
    }

    func test_load_failure_reportsIt() async {
        let service = FakeMuseumService()
        service.fetchResult = .failure(IdentityAPIClientError.invalidResponse)
        let viewModel = makeViewModel(service)

        await viewModel.load()

        guard case .failed = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
    }

    // MARK: - Museum-level mutation

    func test_setMuseumPrivacy_callsThePrivacyEndpoint_andAdoptsTheServerState() async throws {
        let service = makeService(museumPrivacy: .private)
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.setMuseumPrivacy(.public)

        XCTAssertEqual(service.receivedMuseumPrivacies, [.public])
        XCTAssertEqual(try content(viewModel).museumPrivacy, .public)
    }

    func test_makingMuseumPublic_updatesEveryRoomsEffectiveVisibility() async throws {
        let viewModel = makeViewModel(makeService(museumPrivacy: .private))
        await viewModel.load()
        XCTAssertEqual(try content(viewModel).rooms[0].visibility, .hiddenByMuseum)

        await viewModel.setMuseumPrivacy(.public)

        let content = try content(viewModel)
        XCTAssertEqual(content.rooms[0].visibility, .visible)
        XCTAssertEqual(content.rooms[1].visibility, .hiddenByRoom)
    }

    func test_setMuseumPrivacy_toTheStateItAlreadyHas_makesNoRequest() async {
        let service = makeService(museumPrivacy: .private)
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.setMuseumPrivacy(.private)

        XCTAssertEqual(service.changePrivacyCallCount, 0)
    }

    func test_setMuseumPrivacy_failure_keepsTheRealStateAndSaysSo() async throws {
        let service = makeService(museumPrivacy: .private)
        service.changePrivacyResult = .failure(IdentityAPIClientError.invalidResponse)
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.setMuseumPrivacy(.public)

        let content = try content(viewModel)
        XCTAssertEqual(content.museumPrivacy, .private, "a failed change must not appear to have happened")
        XCTAssertEqual(content.notice, "Couldn't change your Museum's privacy. It's still Private.")
        XCTAssertFalse(content.museumIsApplying)
    }

    // MARK: - Room-level mutation

    func test_setRoomPrivacy_sendsAPrivacyOnlyPatch() async throws {
        let service = makeService(museumPrivacy: .public)
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.setPrivacy(.private, forRoomWithID: "r1")

        XCTAssertEqual(service.receivedUpdateRoomIDs, ["r1"])
        XCTAssertEqual(service.receivedRoomPatches, [RoomPatch(privacy: .private)])
        XCTAssertEqual(try content(viewModel).rooms[0].room.privacy, .private)
    }

    func test_setRoomPrivacy_preservesNameVariantPhotosAndCaptions() async throws {
        let service = makeService(museumPrivacy: .public)
        let viewModel = makeViewModel(service)
        await viewModel.load()
        let before = try content(viewModel).rooms[0].room

        await viewModel.setPrivacy(.private, forRoomWithID: "r1")

        let after = try content(viewModel).rooms[0].room
        XCTAssertEqual(after.name, before.name)
        XCTAssertEqual(after.variantID, before.variantID)
        XCTAssertEqual(after.photoSlots, before.photoSlots)
        XCTAssertEqual(after.sculptures, before.sculptures)
        XCTAssertNotEqual(after.privacy, before.privacy)

        let patch = try XCTUnwrap(service.receivedRoomPatches.first)
        XCTAssertNil(patch.name)
        XCTAssertNil(patch.variantID)
    }

    func test_setRoomPrivacy_leavesOtherRoomsAlone() async throws {
        let service = makeService(museumPrivacy: .public)
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.setPrivacy(.private, forRoomWithID: "r1")

        let content = try content(viewModel)
        XCTAssertEqual(content.rooms.count, 2)
        XCTAssertEqual(content.rooms[1].room.privacy, .private)
        XCTAssertEqual(content.rooms[1].room.name, "Study")
        XCTAssertEqual(service.updateRoomCallCount, 1)
    }

    func test_setRoomPrivacy_toTheStateItAlreadyHas_makesNoRequest() async {
        let service = makeService(museumPrivacy: .public)
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.setPrivacy(.public, forRoomWithID: "r1")

        XCTAssertEqual(service.updateRoomCallCount, 0)
    }

    func test_setRoomPrivacy_forAnUnknownRoom_doesNothing() async {
        let service = makeService()
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.setPrivacy(.public, forRoomWithID: "nope")

        XCTAssertEqual(service.updateRoomCallCount, 0)
    }

    func test_setRoomPrivacy_failure_keepsTheRealStateAndSaysSo() async throws {
        let service = makeService(museumPrivacy: .public)
        service.updateRoomResult = .failure(IdentityAPIClientError.invalidResponse)
        let viewModel = makeViewModel(service)
        await viewModel.load()

        await viewModel.setPrivacy(.private, forRoomWithID: "r1")

        let content = try content(viewModel)
        XCTAssertEqual(content.rooms[0].room.privacy, .public)
        XCTAssertEqual(content.notice, "Couldn't change “The Long Hall”. It's still Public.")
        XCTAssertFalse(content.rooms[0].isApplying)
    }

    // MARK: - Exposure confirmation

    func test_makingTheMuseumPublic_confirmsAndNamesHowManyRoomsBecomeVisible() async throws {
        let viewModel = makeViewModel(makeService(museumPrivacy: .private))
        await viewModel.load()

        let confirmation = try XCTUnwrap(viewModel.confirmation(forMuseumTarget: .public))

        XCTAssertEqual(confirmation.title, "Make your Museum Public?")
        XCTAssertTrue(confirmation.message.contains("1 Public Room"), confirmation.message)
    }

    func test_makingTheMuseumPublicWithNoPublicRooms_saysNothingBecomesVisibleYet() async throws {
        let rooms = [Room(id: "r1", name: "Study", variantID: "v", privacy: .private)]
        let viewModel = makeViewModel(makeService(museumPrivacy: .private, rooms: rooms))
        await viewModel.load()

        let confirmation = try XCTUnwrap(viewModel.confirmation(forMuseumTarget: .public))

        XCTAssertTrue(confirmation.message.contains("None of your Rooms are Public yet"), confirmation.message)
    }

    func test_makingTheMuseumPrivate_needsNoConfirmation() async {
        let viewModel = makeViewModel(makeService(museumPrivacy: .public))
        await viewModel.load()

        XCTAssertNil(viewModel.confirmation(forMuseumTarget: .private))
    }

    func test_makingARoomPublic_confirmsOnlyWhenTheMuseumIsPublic() async throws {
        let privateMuseum = makeViewModel(makeService(museumPrivacy: .private))
        await privateMuseum.load()
        let room = Room(id: "r2", name: "Study", variantID: "v2", privacy: .private)

        XCTAssertNil(privateMuseum.confirmation(forRoom: room, target: .public))

        let publicMuseum = makeViewModel(makeService(museumPrivacy: .public))
        await publicMuseum.load()
        let confirmation = try XCTUnwrap(publicMuseum.confirmation(forRoom: room, target: .public))
        XCTAssertEqual(confirmation.title, "Make “Study” Public?")
    }

    func test_makingARoomPrivate_needsNoConfirmation() async {
        let viewModel = makeViewModel(makeService(museumPrivacy: .public))
        await viewModel.load()
        let room = Room(id: "r1", name: "The Long Hall", variantID: "v1", privacy: .public)

        XCTAssertNil(viewModel.confirmation(forRoom: room, target: .private))
    }
}
