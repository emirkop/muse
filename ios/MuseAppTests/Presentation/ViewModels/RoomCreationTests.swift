import XCTest
@testable import MuseApp

final class RoomNamingRulesTests: XCTestCase {
    func test_emptyName_isRejectedGently() {
        XCTAssertEqual(RoomNamingRules.rejection(for: ""), .empty)
        XCTAssertEqual(
            RoomNamingRules.message(for: .empty),
            "Give your Room a name to continue.",
            "`02` requires gentle inline copy, not a blocking modal"
        )
    }

    func test_whitespaceOnlyName_countsAsEmpty() {
        XCTAssertEqual(RoomNamingRules.rejection(for: "   \n "), .empty)
    }

    func test_ordinaryPersonalNames_areAccepted() {
        for name in ["Trabzon", "Games", "My Brother", "1998", "café ☕️"] {
            XCTAssertNil(RoomNamingRules.rejection(for: name), "\(name) should be a valid Room name")
        }
    }

    func test_overlyLongName_isRejectedAtTheInterimLimit() {
        let atLimit = String(repeating: "a", count: RoomNamingRules.interimMaximumLength)
        let overLimit = String(repeating: "a", count: RoomNamingRules.interimMaximumLength + 1)

        XCTAssertNil(RoomNamingRules.rejection(for: atLimit))
        XCTAssertEqual(
            RoomNamingRules.rejection(for: overLimit),
            .tooLong(limit: RoomNamingRules.interimMaximumLength)
        )
    }

    func test_uniquenessIsNotEnforced_perTheConfirmedUXFlow() {
        XCTAssertFalse(RoomNamingRules.interimEnforcesUniqueness)
    }

    func test_noProfanityFilterIsApplied_whileTheGateIsOpen() {
        XCTAssertFalse(RoomNamingRules.interimAppliesProfanityFilter)
    }

    func test_placeholderIsAnExample_notADefaultValue() {
        XCTAssertTrue(RoomNamingRules.placeholderExample.hasPrefix("e.g."))
    }
}

@MainActor
final class RoomCreationViewModelTests: XCTestCase {

    func test_confirmedName_returnsTrimmedName() {
        let viewModel = RoomCreationViewModel()
        viewModel.updateName("  Games  ")

        XCTAssertEqual(viewModel.confirmedName(), "Games")
        XCTAssertNil(viewModel.nameValidationMessage)
    }

    func test_emptyName_isRejectedWithInlineValidation() {
        let viewModel = RoomCreationViewModel()
        viewModel.updateName("")

        XCTAssertNil(viewModel.confirmedName(), "an unnamed Room must not advance")
        XCTAssertEqual(viewModel.nameValidationMessage, "Give your Room a name to continue.")
    }

    func test_whitespaceOnlyName_isRejected() {
        let viewModel = RoomCreationViewModel()
        viewModel.updateName("    ")

        XCTAssertNil(viewModel.confirmedName())
    }

    func test_overlyLongName_isRejected() {
        let viewModel = RoomCreationViewModel()
        viewModel.updateName(String(repeating: "a", count: RoomNamingRules.interimMaximumLength + 1))

        XCTAssertNil(viewModel.confirmedName())
        XCTAssertTrue(viewModel.isOverLengthLimit)
    }

    func test_validationMessageClearsOnceTheNameBecomesValid() {
        let viewModel = RoomCreationViewModel()
        viewModel.updateName("")
        _ = viewModel.confirmedName()
        XCTAssertNotNil(viewModel.nameValidationMessage)

        viewModel.updateName("Trabzon")

        XCTAssertNil(viewModel.nameValidationMessage)
    }

    func test_liveCharacterFeedbackReflectsTheInterimLimit() {
        let viewModel = RoomCreationViewModel()

        viewModel.updateName("Trabzon")
        XCTAssertEqual(viewModel.characterCountText, "7/\(RoomNamingRules.interimMaximumLength)")
        XCTAssertFalse(viewModel.isOverLengthLimit)
    }

    // MARK: - Open product gates (, still open)

    func test_zeroPhotoGate_isSurfacedWithoutBeingAnswered() {
        XCTAssertTrue(RoomCreationViewModel.zeroPhotoRoomsGateNotice.contains("isn't settled"))
    }

    func test_noRoomCountCapIsInvented() {
        XCTAssertNil(RoomCreationViewModel.roomCountCap)
    }
}

@MainActor
final class RoomVariantSelectionViewModelTests: XCTestCase {
    private let existingRoom = Room(
        id: "r1", name: "Trabzon", variantID: "v1", privacy: .public,
        photoSlots: [PhotoSlotAssignment(slotIndex: 0, photoAssetID: "asset_a", caption: "sunset")]
    )

    private func makeViewModel(
        _ service: FakeMuseumService,
        context: RoomVariantSelectionViewModel.Context
    ) -> RoomVariantSelectionViewModel {
        RoomVariantSelectionViewModel(
            context: context,
            museumService: service,
            catalogService: service,
            accessToken: "token",
            styleID: "style_modern"
        )
    }

    // MARK: - Style scoping

    func test_variantsAreScopedToTheMuseumsActiveStyle() async {
        let service = FakeMuseumService()
        let viewModel = makeViewModel(service, context: .creatingRoom(name: "Trabzon"))

        await viewModel.load()

        XCTAssertEqual(service.receivedVariantStyleIDs, ["style_modern"])
    }

    // MARK: - Creating context

    func test_creatingContext_createsTheRoomWithTheChosenVariant() async {
        let service = FakeMuseumService()
        let viewModel = makeViewModel(service, context: .creatingRoom(name: "Trabzon"))
        await viewModel.load()

        await viewModel.chooseVariant("v2")

        XCTAssertEqual(service.receivedRoomNames, ["Trabzon"])
        XCTAssertEqual(service.receivedRoomVariantIDs, ["v2"])
        XCTAssertEqual(service.updateRoomCallCount, 0, "creating must not use the update endpoint")
    }

    func test_creatingContext_hasNoCurrentVariantAndNoReassurance() {
        let viewModel = makeViewModel(FakeMuseumService(), context: .creatingRoom(name: "Trabzon"))

        XCTAssertNil(viewModel.currentVariantID)
        XCTAssertNil(viewModel.confirmationReassurance)
    }

    // MARK: - Changing context

    func test_changingContext_updatesTheRoom_ratherThanCreatingASecondOne() async {
        let service = FakeMuseumService()
        let viewModel = makeViewModel(service, context: .changingVariant(room: existingRoom))
        await viewModel.load()

        await viewModel.chooseVariant("v2")

        XCTAssertEqual(service.updateRoomCallCount, 1)
        XCTAssertEqual(service.createRoomCallCount, 0, "changing a design must never create a Room")
    }

    func test_changingVariant_carriesExistingContentUnchanged() async {
        let service = FakeMuseumService()
        let viewModel = makeViewModel(service, context: .changingVariant(room: existingRoom))
        await viewModel.load()

        await viewModel.chooseVariant("v2")

        XCTAssertEqual(service.receivedUpdateRoomIDs, ["r1"])
        XCTAssertEqual(service.receivedRoomPatches, [RoomPatch(variantID: "v2")])
        let patch = service.receivedRoomPatches.first
        XCTAssertNil(patch?.name, "a design change must not be able to rewrite the Room name")
        XCTAssertNil(patch?.privacy, "a design change must not be able to alter privacy")
    }

    func test_changingVariant_preservesPhotos() async {
        let service = FakeMuseumService()
        service.updateRoomResult = .success(
            Room(
                id: "r1", name: "Trabzon", variantID: "v2", privacy: .public,
                photoSlots: existingRoom.photoSlots
            )
        )
        let viewModel = makeViewModel(service, context: .changingVariant(room: existingRoom))
        await viewModel.load()

        await viewModel.chooseVariant("v2")

        guard case .applied(let room) = viewModel.state else {
            XCTFail("expected .applied, got \(viewModel.state)")
            return
        }
        XCTAssertEqual(room.variantID, "v2", "the presentation reference changed")
        XCTAssertEqual(room.photoSlots, existingRoom.photoSlots, "the photos did not")
    }

    func test_changingContext_marksTheAppliedVariantAsCurrent_andReassures() async {
        let service = FakeMuseumService()
        let viewModel = makeViewModel(service, context: .changingVariant(room: existingRoom))
        await viewModel.load()

        guard case .ready(let variants) = viewModel.state else {
            XCTFail("expected .ready")
            return
        }
        let current = try? XCTUnwrap(variants.first { $0.id == "v1" })
        let other = try? XCTUnwrap(variants.first { $0.id == "v2" })
        XCTAssertTrue(viewModel.isCurrentlySelected(current!))
        XCTAssertFalse(viewModel.isCurrentlySelected(other!))
        XCTAssertEqual(viewModel.confirmationReassurance, "Your photos stay exactly where they are.")
    }

    // MARK: - Failure handling

    func test_failure_preservesVariantsForRetry() async {
        let service = FakeMuseumService()
        service.createRoomResult = .failure(IdentityAPIClientError.transport)
        let viewModel = makeViewModel(service, context: .creatingRoom(name: "Trabzon"))
        await viewModel.load()

        await viewModel.chooseVariant("v1")

        guard case .failed(_, let variants) = viewModel.state else {
            XCTFail("expected .failed, got \(viewModel.state)")
            return
        }
        XCTAssertFalse(variants.isEmpty)
    }
}

@MainActor
final class RoomListViewModelTests: XCTestCase {
    func test_emptyMuseum_isANormalState_notAFailure() async {
        let service = FakeMuseumService()
        service.roomsResult = .success([])
        let viewModel = RoomListViewModel(museumService: service, accessToken: "token")

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .loaded(rooms: []))
        XCTAssertTrue(viewModel.canCreateRoom)
    }

    func test_listsCreatedRooms() async {
        let service = FakeMuseumService()
        service.roomsResult = .success([
            Room(id: "r1", name: "Trabzon", variantID: "v1", privacy: .private),
            Room(id: "r2", name: "Games", variantID: "v1", privacy: .private)
        ])
        let viewModel = RoomListViewModel(museumService: service, accessToken: "token")

        await viewModel.load()

        guard case .loaded(let rooms) = viewModel.state else {
            XCTFail("expected .loaded, got \(viewModel.state)")
            return
        }
        XCTAssertEqual(rooms.map(\.name), ["Trabzon", "Games"])
    }

    func test_creationAlwaysOffered_whileTheRoomCapGateIsOpen() async {
        let service = FakeMuseumService()
        service.roomsResult = .success((0..<50).map {
            Room(id: "r\($0)", name: "Room \($0)", variantID: "v1", privacy: .private)
        })
        let viewModel = RoomListViewModel(museumService: service, accessToken: "token")

        await viewModel.load()

        XCTAssertTrue(viewModel.canCreateRoom, "no cap may be enforced while is open")
    }

    func test_loadFailure_isRetryable() async {
        let service = FakeMuseumService()
        service.roomsResult = .failure(IdentityAPIClientError.transport)
        let viewModel = RoomListViewModel(museumService: service, accessToken: "token")

        await viewModel.load()

        guard case .failed = viewModel.state else {
            XCTFail("expected .failed, got \(viewModel.state)")
            return
        }
    }
}
