import simd
import XCTest
@testable import MuseApp

@MainActor
final class LobbyEntryViewModelTests: XCTestCase {
    private struct StubLobbyGeometry: LobbyGeometryProviding {
        let geometry: LobbyRuntimeContent.Geometry?
        func lobbyGeometry(forStyleID styleID: String) async -> LobbyRuntimeContent.Geometry? { geometry }
    }

    private struct StubCardTables: LobbyCardTableProviding {
        let table: LobbyCardTable?
        func cardTable(forStyleID styleID: String, cardCount: Int) async -> LobbyCardTable? { table }
    }

    private func room(_ id: String, _ name: String, _ privacy: MusePrivacy) -> Room {
        Room(id: id, name: name, variantID: "variant_a", privacy: privacy)
    }

    private func service(styleID: String = "style_modern", rooms: [Room]) -> FakeMuseumService {
        let service = FakeMuseumService()
        service.fetchResult = .success(Museum(id: "m1", styleID: styleID, privacy: .private))
        service.roomsResult = .success(rooms)
        return service
    }

    private func table(forStyle styleID: String, spots: Int = 8) -> LobbyCardTable {
        LobbyCardTable(
            styleID: styleID,
            cardSpots: (0..<spots).map { SlotTransform(position: SIMD3<Float>(Float($0) * 4, 1.5, 0)) }
        )
    }

    private func makeViewModel(
        viewerRole: MuseumViewerRole = .owner,
        service: FakeMuseumService,
        styleID: String = "style_modern",
        geometry: LobbyRuntimeContent.Geometry? = .verificationFixture,
        table: LobbyCardTable? = nil
    ) -> LobbyEntryViewModel {
        LobbyEntryViewModel(
            viewerRole: viewerRole,
            contentSource: OwnedMuseumLobbyContent(museumService: service),
            geometry: StubLobbyGeometry(geometry: geometry),
            cardTables: StubCardTables(table: table ?? self.table(forStyle: styleID)),
            accessToken: "token"
        )
    }

    // MARK: - routing, end to end through the view model

    func test_noRooms_reportsTheEmptyState_withoutAskingForGeometry() async {
        let viewModel = makeViewModel(service: service(rooms: []), geometry: nil)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .noVisibleRooms)
        XCTAssertNil(viewModel.content)
    }

    func test_oneRoom_entersItDirectly_evenWithNoLobbyDesignAtAll() async {
        let only = room("r1", "Studio", .public)
        let viewModel = LobbyEntryViewModel(
            viewerRole: .owner,
            contentSource: OwnedMuseumLobbyContent(museumService: service(rooms: [only])),
            geometry: UnavailableLobbyGeometryProvider(),
            cardTables: UnavailableLobbyCardTableProvider(),
            accessToken: "token"
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .enterRoomDirectly(only))
    }

    func test_twoRooms_withDesignAvailable_isReady() async {
        let viewModel = makeViewModel(service: service(rooms: [
            room("r1", "One", .public),
            room("r2", "Two", .public)
        ]))

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertEqual(viewModel.content?.placements.count, 2)
        XCTAssertEqual(viewModel.content?.cards.map(\.roomID), ["r1", "r2"])
    }

    // MARK: - The honest unavailable states

    func test_withProductionProviders_multiRoomMuseumReportsLobbyUnavailable() async {
        let viewModel = LobbyEntryViewModel(
            viewerRole: .owner,
            contentSource: OwnedMuseumLobbyContent(museumService: service(rooms: [
                room("r1", "One", .public),
                room("r2", "Two", .public)
            ])),
            geometry: UnavailableLobbyGeometryProvider(),
            cardTables: UnavailableLobbyCardTableProvider(),
            accessToken: "token"
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .lobbyDesignUnavailable(styleID: "style_modern"))
        XCTAssertNil(viewModel.content, "a real Museum must never be handed the fixture hall")
    }

    func test_geometryWithoutACardTable_isStillUnavailable() async {
        let viewModel = LobbyEntryViewModel(
            viewerRole: .owner,
            contentSource: OwnedMuseumLobbyContent(museumService: service(rooms: [room("r1", "One", .public), room("r2", "Two", .public)])),
            geometry: StubLobbyGeometry(geometry: .verificationFixture),
            cardTables: UnavailableLobbyCardTableProvider(),
            accessToken: "token"
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .lobbyDesignUnavailable(styleID: "style_modern"))
    }

    func test_tableTooSmallForTheMuseum_reportsThePlacementFailure() async {
        let viewModel = makeViewModel(
            service: service(rooms: (0..<4).map { room("r\($0)", "Room \($0)", .public) }),
            table: LobbyCardTable(styleID: "style_modern", cardSpots: [SlotTransform(position: .zero)])
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .placementUnresolvable(.insufficientCardSpots(needed: 4, available: 1)))
    }

    func test_wrongStylesTable_reportsAMismatch() async {
        let viewModel = makeViewModel(
            service: service(styleID: "style_modern", rooms: [
                room("r1", "One", .public), room("r2", "Two", .public)
            ]),
            table: PlaceholderLobbyCardTable.build(cardCount: 4)
        )

        await viewModel.load()

        XCTAssertEqual(
            viewModel.state,
            .placementUnresolvable(.styleMismatch(expected: "style_modern", received: PlaceholderLobbyCardTable.styleID)),
            "the fixture table must be refused for a real Style"
        )
    }

    func test_museumFetchFailure_isReportedNotSwallowed() async {
        let service = FakeMuseumService()
        service.fetchResult = .failure(IdentityAPIClientError.invalidResponse)
        let viewModel = makeViewModel(service: service)

        await viewModel.load()

        guard case .failed = viewModel.state else {
            return XCTFail("a failed Museum load must be reported, not shown as an empty Lobby")
        }
    }

    // MARK: - The filtering hook, through the view model

    func test_visitorSeesOnlyPublicRooms() async {
        let viewModel = makeViewModel(
            viewerRole: .visitor,
            service: service(rooms: [
                room("r1", "Public One", .public),
                room("r2", "Private", .private),
                room("r3", "Public Two", .public)
            ])
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready)
        XCTAssertEqual(viewModel.content?.cards.map(\.roomID), ["r1", "r3"])
    }

    func test_ownerSeesPrivateRoomsMarked() async {
        let viewModel = makeViewModel(service: service(rooms: [
            room("r1", "Public", .public),
            room("r2", "Private", .private)
        ]))

        await viewModel.load()

        XCTAssertEqual(viewModel.content?.cards.map(\.isMarkedPrivate), [false, true])
        XCTAssertEqual(viewModel.contentSummary, "2 Rooms · 1 Private")
    }

    func test_sameMuseumRoutesDifferentlyPerRole() async {
        let rooms = [room("r1", "Public", .public), room("r2", "Private", .private)]

        let ownerViewModel = makeViewModel(viewerRole: .owner, service: service(rooms: rooms))
        let visitorViewModel = makeViewModel(viewerRole: .visitor, service: service(rooms: rooms))
        await ownerViewModel.load()
        await visitorViewModel.load()

        XCTAssertEqual(ownerViewModel.state, .ready)
        XCTAssertEqual(visitorViewModel.state, .enterRoomDirectly(rooms[0]))
    }

    func test_visitorWithNoPublicRooms_getsTheEmptyStateCopyFromO2() async {
        let viewModel = makeViewModel(
            viewerRole: .visitor,
            service: service(rooms: [room("r1", "Private", .private)])
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .noVisibleRooms)
        XCTAssertEqual(viewModel.noVisibleRoomsMessage, "No public Rooms yet.")
    }

    func test_ownerWithNoRooms_getsOwnerCopy() async {
        let viewModel = makeViewModel(service: service(rooms: []))

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .noVisibleRooms)
        XCTAssertTrue(viewModel.noVisibleRoomsMessage.contains("no Rooms yet"))
    }

    func test_viewerRoleIsCarriedIntoRuntimeContent() async {
        let viewModel = makeViewModel(
            viewerRole: .visitor,
            service: service(rooms: (0..<3).map { room("r\($0)", "Room \($0)", .public) })
        )

        await viewModel.load()

        XCTAssertEqual(viewModel.content?.viewerRole, .visitor)
    }

    func test_reloadingAfterFailure_recovers() async {
        let service = FakeMuseumService()
        service.fetchResult = .failure(IdentityAPIClientError.invalidResponse)
        let viewModel = makeViewModel(service: service)

        await viewModel.load()
        service.fetchResult = .success(Museum(id: "m1", styleID: "style_modern", privacy: .private))
        service.roomsResult = .success([room("r1", "One", .public), room("r2", "Two", .public)])
        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready)
    }
}
