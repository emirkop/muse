import simd
import XCTest
@testable import MuseApp

@MainActor
final class VisitorLobbyTests: XCTestCase {
    private struct StubLobbyGeometry: LobbyGeometryProviding {
        let geometry: LobbyRuntimeContent.Geometry?
        func lobbyGeometry(forStyleID styleID: String) async -> LobbyRuntimeContent.Geometry? { geometry }
    }

    private struct StubCardTables: LobbyCardTableProviding {
        let table: LobbyCardTable?
        func cardTable(forStyleID styleID: String, cardCount: Int) async -> LobbyCardTable? { table }
    }

    private func table(spots: Int = 8) -> LobbyCardTable {
        LobbyCardTable(
            styleID: "style_modern",
            cardSpots: (0..<spots).map { SlotTransform(position: SIMD3<Float>(Float($0) * 4, 1.5, 0)) }
        )
    }

    private func sharedService(rooms: [SharedRoomSummary]) -> FakeShareLinkService {
        let service = FakeShareLinkService()
        service.sharedMuseumResult = .success(
            SharedMuseumContent(museumID: "m1", styleID: "style_modern", rooms: rooms)
        )
        return service
    }

    private func visitorViewModel(
        service: FakeShareLinkService,
        geometry: LobbyRuntimeContent.Geometry? = .verificationFixture
    ) -> LobbyEntryViewModel {
        LobbyEntryViewModel(
            viewerRole: .visitor,
            contentSource: SharedMuseumLobbyContent(shareLinkService: service, code: "abcdefghijklmnopqrstuv"),
            geometry: StubLobbyGeometry(geometry: geometry),
            cardTables: StubCardTables(table: table()),
            accessToken: "token"
        )
    }

    // MARK: - The Lobby carries only what the server authorized

    func test_visitorLobby_showsExactlyTheServersAuthorizedRooms() async {
        let service = sharedService(rooms: [
            SharedRoomSummary(id: "r1", name: "Long Hall", variantID: "v1"),
            SharedRoomSummary(id: "r3", name: "Atrium", variantID: "v1")
        ])
        let viewModel = visitorViewModel(service: service)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertEqual(viewModel.content?.cards.map(\.roomID), ["r1", "r3"])
        XCTAssertEqual(viewModel.content?.viewerRole, .visitor)
        XCTAssertTrue(viewModel.content?.cards.allSatisfy { !$0.isMarkedPrivate } == true)
        XCTAssertEqual(service.sharedMuseumRequests.map(\.code), ["abcdefghijklmnopqrstuv"])
    }

    func test_visitorLobby_readsThroughTheLink_notTheOwnMuseumRoute() async {
        let museumService = FakeMuseumService()
        museumService.fetchResult = .success(Museum(id: "mine", styleID: "style_gothic", privacy: .private))
        museumService.roomsResult = .success([Room(id: "mine-r1", name: "Mine", variantID: "v1", privacy: .private)])
        let shared = sharedService(rooms: [
            SharedRoomSummary(id: "r1", name: "Theirs", variantID: "v1"),
            SharedRoomSummary(id: "r2", name: "Theirs Too", variantID: "v1")
        ])

        let viewModel = LobbyEntryViewModel(
            viewerRole: .visitor,
            contentSource: SharedMuseumLobbyContent(shareLinkService: shared, code: "abcdefghijklmnopqrstuv"),
            geometry: StubLobbyGeometry(geometry: .verificationFixture),
            cardTables: StubCardTables(table: table()),
            accessToken: "token"
        )
        await viewModel.load()

        XCTAssertEqual(viewModel.content?.museumID, "m1", "the visited Museum, not the visitor's own")
        XCTAssertEqual(viewModel.content?.cards.map(\.name), ["Theirs", "Theirs Too"])
        XCTAssertEqual(viewModel.content?.styleID, "style_modern", "the visited Museum's Style")
    }

    func test_visitorWithOneVisibleRoom_entersItDirectly() async {
        let service = sharedService(rooms: [SharedRoomSummary(id: "r1", name: "Studio", variantID: "v1")])
        let viewModel = LobbyEntryViewModel(
            viewerRole: .visitor,
            contentSource: SharedMuseumLobbyContent(shareLinkService: service, code: "abcdefghijklmnopqrstuv"),
            geometry: UnavailableLobbyGeometryProvider(),
            cardTables: UnavailableLobbyCardTableProvider(),
            accessToken: "token"
        )

        await viewModel.load()

        guard case .enterRoomDirectly(let room) = viewModel.state else {
            return XCTFail("expected the single-Room skip, got \(viewModel.state)")
        }
        XCTAssertEqual(room.id, "r1")
        XCTAssertEqual(room.privacy, .public, "a Room the server returned to a visitor is Public by definition")
    }

    func test_visitorWithNoVisibleRooms_getsTheExplicitEmptyState() async {
        let viewModel = visitorViewModel(service: sharedService(rooms: []), geometry: nil)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .noVisibleRooms)
        XCTAssertEqual(viewModel.noVisibleRoomsMessage, "No public Rooms yet.")
        XCTAssertTrue(viewModel.isVisitor)
    }

    func test_visitorLobby_whenTheLinkStopsResolving_reportsNoLongerAvailable() async {
        let service = FakeShareLinkService()
        service.sharedMuseumResult = .failure(IdentityAPIClientError.server(statusCode: 404, message: "not found"))
        let viewModel = visitorViewModel(service: service)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .noLongerAvailable)
        XCTAssertEqual(viewModel.noLongerAvailableMessage, "This link is no longer available.")
    }

    func test_visitorLobby_transportFailure_isNotConfusedWithADeadLink() async {
        let service = FakeShareLinkService()
        service.sharedMuseumResult = .failure(IdentityAPIClientError.transport)
        let viewModel = visitorViewModel(service: service)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .failed(message: "Couldn't open this Museum. Please try again."))
    }

    func test_ownerLobby_isUnchanged() async {
        let museumService = FakeMuseumService()
        museumService.fetchResult = .success(Museum(id: "m1", styleID: "style_modern", privacy: .private))
        museumService.roomsResult = .success([
            Room(id: "r1", name: "Public Hall", variantID: "v1", privacy: .public),
            Room(id: "r2", name: "Private Study", variantID: "v1", privacy: .private)
        ])
        let viewModel = LobbyEntryViewModel(
            viewerRole: .owner,
            contentSource: OwnedMuseumLobbyContent(museumService: museumService),
            geometry: StubLobbyGeometry(geometry: .verificationFixture),
            cardTables: StubCardTables(table: table()),
            accessToken: "token"
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.content?.cards.map(\.roomID), ["r1", "r2"])
        XCTAssertEqual(viewModel.content?.cards.map(\.isMarkedPrivate), [false, true])
        XCTAssertFalse(viewModel.isVisitor)
        XCTAssertEqual(viewModel.contentSummary, "2 Rooms · 1 Private")
    }
}

@MainActor
final class VisitorRoomTests: XCTestCase {
    private struct FixtureDesign: RoomDesignProviding {
        func design(forVariantID variantID: String, progress: @escaping @Sendable (RoomDesignLoadState) -> Void) async -> RoomDesignResolution {
            .available(RoomDesign(slotTable: PlaceholderRoomSlotTable.build(), geometry: .verificationFixture))
        }
    }

    private struct StubTextures: RoomPhotoTextureProviding {
        func textures(
            for placements: [ResolvedPhotoPlacement],
            roomID: String,
            accessToken: String,
            maxLongEdge: Int
        ) -> AsyncStream<RoomPhotoTextureEvent> {
            AsyncStream { $0.finish() }
        }
    }

    private func room(photoCount: Int = 2) -> Room {
        Room(
            id: "r1", name: "Long Hall", variantID: PlaceholderRoomSlotTable.variantID, privacy: .public,
            photoSlots: (0..<photoCount).map {
                PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset\($0)", caption: "")
            }
        )
    }

    func test_visitorRoomContent_supportsNoEditingOfAnyKind() async {
        let viewModel = RoomEntryViewModel(
            room: room(),
            viewerRole: .visitor,
            design: FixtureDesign(),
            textures: StubTextures(),
            accessToken: "token",
            sculptureModels: UnavailableSculptureModelProvider()
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.state, RoomEntryViewModel.State.ready)
        let content = viewModel.content
        XCTAssertNotNil(content)
        XCTAssertEqual(content?.viewerRole, .visitor)
        XCTAssertEqual(content?.supportsOwnerEditing, false)
        XCTAssertEqual(content?.supportsPhotoReplacement, false)
        XCTAssertEqual(content?.supportsSculptureEditing, false)
        XCTAssertNil(content?.photoService, "a visitor is handed nothing that can mutate")
        XCTAssertNil(content?.roomService)
        XCTAssertNil(content?.photoReplacer)
        XCTAssertNil(content?.catalogService)
    }

    func test_visitorRole_deniesEditing_evenWithEveryServicePresent() async {
        let museumService = FakeMuseumService()
        let photoService = FakeRoomPhotoService()
        let viewModel = RoomEntryViewModel(
            room: room(),
            viewerRole: .visitor,
            design: FixtureDesign(),
            textures: StubTextures(),
            accessToken: "token",
            photoService: photoService,
            roomService: museumService,
            catalogService: museumService
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.content?.supportsOwnerEditing, false)
        XCTAssertEqual(viewModel.content?.supportsSculptureEditing, false)
    }

    func test_ownerRoomContent_stillSupportsEditing() async {
        let museumService = FakeMuseumService()
        let photoService = FakeRoomPhotoService()
        let viewModel = RoomEntryViewModel(
            room: room(),
            design: FixtureDesign(),
            textures: StubTextures(),
            accessToken: "token",
            photoService: photoService,
            roomService: museumService,
            catalogService: museumService
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.content?.viewerRole, .owner)
        XCTAssertEqual(viewModel.content?.supportsOwnerEditing, true)
        XCTAssertEqual(viewModel.content?.supportsSculptureEditing, true)
    }

    func test_visitorTicketSource_canExpressNothingButDelivery() async throws {
        let service = FakeShareLinkService()
        service.sharedRoomTickets["r1"] = [
            PhotoDownloadTicket(
                photoAssetID: "asset0",
                url: URL(string: "https://cdn.example/asset0")!,
                expiresAt: Date(timeIntervalSinceNow: 300),
                pixelWidth: 640,
                pixelHeight: 480
            )
        ]
        let source: any RoomPhotoTicketing = SharedRoomPhotoTickets(shareLinkService: service, code: "abcdefghijklmnopqrstuv")

        let tickets = try await source.fetchPhotoURLs(accessToken: "token", roomID: "r1")

        XCTAssertEqual(tickets.map(\.photoAssetID), ["asset0"])
        XCTAssertEqual(service.sharedRoomTicketRequests.map(\.code), ["abcdefghijklmnopqrstuv"])
        do {
            _ = try await source.fetchPhotoURLs(accessToken: "token", roomID: "r-private")
            XCTFail("expected a refusal")
        } catch IdentityAPIClientError.server(let statusCode, _) {
            XCTAssertEqual(statusCode, 404)
        }
    }
}

@MainActor
final class VisitorRoomRuntimeTests: XCTestCase {
    private func controller(role: RoomViewerRole, withServices: Bool) -> RealityKitSceneViewController {
        let fixture = RoomRenderingVerificationFixture.makeContent(photoCount: 3)!
        let content = RoomRuntimeContent(
            roomID: fixture.roomID,
            accessToken: fixture.accessToken,
            geometry: fixture.geometry,
            viewerRole: role,
            room: fixture.room,
            slotTable: fixture.slotTable,
            placements: fixture.placements,
            textures: fixture.textures,
            photoService: withServices ? fixture.photoService : nil,
            roomService: withServices ? fixture.roomService : nil
        )
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        return controller
    }

    private func barButtonIdentifiers(_ controller: UIViewController) -> [String] {
        var items: [UIBarButtonItem] = []
        if let right = controller.navigationItem.rightBarButtonItem { items.append(right) }
        items.append(contentsOf: controller.navigationItem.rightBarButtonItems ?? [])
        return items.compactMap(\.accessibilityIdentifier)
    }

    func test_visitorRoom_createsNoOwnerAffordanceAtAll() {
        let visitor = controller(role: .visitor, withServices: true)

        XCTAssertNil(visitor.editMode, "Edit Mode must not exist for a visitor")
        XCTAssertNil(visitor.contentCoordinator, "no reorder/caption/replace/delete coordinator")
        XCTAssertNil(visitor.reorderInteraction, "no long-press reorder gesture")
        XCTAssertNil(visitor.photoTapInteraction, "no photo action set")
        XCTAssertNil(visitor.sculptureCoordinator, "no sculpture editing")
        XCTAssertFalse(barButtonIdentifiers(visitor).contains("room-edit-mode-toggle"))
        XCTAssertFalse(barButtonIdentifiers(visitor).contains("room-sculptures-action"))
    }

    func test_visitorRoom_stillRendersTheRoomsContent() {
        let visitor = controller(role: .visitor, withServices: true)

        XCTAssertNotNil(visitor.photoLayer, "a visitor sees the photographs")
        XCTAssertNotNil(visitor.sculptureLayer, "a visitor sees the sculptures")
        XCTAssertNotNil(visitor.captionLayer, "a visitor reads the captions")
    }

    func test_ownerRoom_stillCreatesItsControls() {
        let owner = controller(role: .owner, withServices: true)

        XCTAssertNotNil(owner.editMode)
        XCTAssertNotNil(owner.contentCoordinator)
        XCTAssertNotNil(owner.reorderInteraction)
        XCTAssertNotNil(owner.photoTapInteraction)
        XCTAssertTrue(barButtonIdentifiers(owner).contains("room-edit-mode-toggle"))
    }

    func test_ownerWithoutServices_getsNoEditingEither() {
        let owner = controller(role: .owner, withServices: false)

        XCTAssertNil(owner.contentCoordinator)
        XCTAssertNil(owner.reorderInteraction)
    }
}
