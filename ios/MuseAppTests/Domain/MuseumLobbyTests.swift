import XCTest
@testable import MuseApp

final class MuseumLobbyTests: XCTestCase {
    private func room(_ id: String, _ name: String, _ privacy: MusePrivacy) -> Room {
        Room(id: id, name: name, variantID: "variant_a", privacy: privacy)
    }

    // MARK: - Filtering (owner)

    func test_owner_seesEveryRoom() {
        let rooms = [
            room("r1", "Public One", .public),
            room("r2", "Private One", .private),
            room("r3", "Public Two", .public)
        ]

        let cards = MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .owner)

        XCTAssertEqual(cards.map(\.roomID), ["r1", "r2", "r3"])
    }

    func test_owner_privateRoomsAreMarked_publicOnesAreNot() {
        let rooms = [room("r1", "Public", .public), room("r2", "Private", .private)]

        let cards = MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .owner)

        XCTAssertFalse(cards[0].isMarkedPrivate)
        XCTAssertTrue(cards[1].isMarkedPrivate, "`02`: the owner sees all Rooms, Private marked")
    }

    // MARK: - Filtering (visitor)

    func test_visitor_privateRoomsAreAbsentEntirely_notLocked() {
        let rooms = [
            room("r1", "Public One", .public),
            room("r2", "Private One", .private),
            room("r3", "Public Two", .public)
        ]

        let cards = MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .visitor)

        XCTAssertEqual(
            cards.map(\.roomID), ["r1", "r3"],
            "`02`: Private Rooms are omitted entirely, so their existence isn't implied"
        )
    }

    func test_visitor_neverSeesAPrivateMarking() {
        let rooms = [room("r1", "Public", .public), room("r2", "Private", .private)]

        let cards = MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .visitor)

        XCTAssertTrue(cards.allSatisfy { !$0.isMarkedPrivate }, "a marking would imply the Private Rooms exist")
    }

    func test_visitor_allPrivate_yieldsNoCards() {
        let rooms = [room("r1", "A", .private), room("r2", "B", .private)]

        XCTAssertTrue(MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .visitor).isEmpty)
    }

    // MARK: - Order and naming

    func test_orderIsPreservedExactly_noSortingIsInvented() {
        let rooms = [
            room("r1", "Zebra", .public),
            room("r2", "Alpha", .public),
            room("r3", "Mango", .public)
        ]

        let cards = MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .owner)

        XCTAssertEqual(
            cards.map(\.name), ["Zebra", "Alpha", "Mango"],
            "no product decision assigns the Lobby an ordering, so none is invented"
        )
    }

    func test_signage_fallsBackForAnEmptyName() {
        let cards = MuseumLobbyVisibility.visibleCards(rooms: [room("r1", "", .public)], viewerRole: .owner)

        XCTAssertEqual(cards[0].signageText, "Untitled Room")
    }

    func test_signage_trimsSurroundingWhitespaceOnly() {
        let cards = MuseumLobbyVisibility.visibleCards(
            rooms: [room("r1", "  My Room  ", .public), room("r2", "   ", .public)],
            viewerRole: .owner
        )

        XCTAssertEqual(cards[0].signageText, "My Room")
        XCTAssertEqual(cards[1].signageText, "Untitled Room", "whitespace-only is as unnamed as empty")
        XCTAssertEqual(cards[0].name, "  My Room  ", "the Room's own name is never rewritten ( is OPEN)")
    }

    // MARK: - Routing

    func test_zeroVisibleRooms_routesToTheEmptyState() {
        XCTAssertEqual(MuseumLobbyRouting.route(forVisible: []), .noVisibleRooms)
    }

    func test_exactlyOneVisibleRoom_skipsTheLobby() {
        let cards = [LobbyRoomCard(roomID: "r1", name: "Only", isMarkedPrivate: false)]

        XCTAssertEqual(MuseumLobbyRouting.route(forVisible: cards), .enterRoomDirectly(roomID: "r1"))
    }

    func test_twoVisibleRooms_routeToTheLobby() {
        let cards = [
            LobbyRoomCard(roomID: "r1", name: "One", isMarkedPrivate: false),
            LobbyRoomCard(roomID: "r2", name: "Two", isMarkedPrivate: false)
        ]

        XCTAssertEqual(MuseumLobbyRouting.route(forVisible: cards), .lobby(cards: cards))
    }

    func test_manyVisibleRooms_routeToTheLobby() {
        let cards = (0..<7).map { LobbyRoomCard(roomID: "r\($0)", name: "Room \($0)", isMarkedPrivate: false) }

        guard case .lobby(let routed) = MuseumLobbyRouting.route(forVisible: cards) else {
            return XCTFail("7 visible Rooms must route to the Lobby")
        }
        XCTAssertEqual(routed.count, 7)
    }

    func test_routingIsOnVisibleCount_notTotalRoomCount() {
        let rooms = [
            room("r1", "Public", .public),
            room("r2", "Private A", .private),
            room("r3", "Private B", .private),
            room("r4", "Private C", .private)
        ]

        let asVisitor = MuseumLobbyRouting.route(
            forVisible: MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .visitor)
        )
        let asOwner = MuseumLobbyRouting.route(
            forVisible: MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .owner)
        )

        XCTAssertEqual(
            asVisitor, .enterRoomDirectly(roomID: "r1"),
            "a visitor who can see one Room skips the Lobby even though the Museum holds four"
        )
        guard case .lobby(let cards) = asOwner else {
            return XCTFail("the same Museum must still give its owner a Lobby")
        }
        XCTAssertEqual(cards.count, 4)
    }

    func test_visitorWithNoPublicRooms_getsTheEmptyStateNotARoom() {
        let rooms = [room("r1", "A", .private), room("r2", "B", .private)]

        let route = MuseumLobbyRouting.route(
            forVisible: MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .visitor)
        )

        XCTAssertEqual(route, .noVisibleRooms)
    }

    func test_filteringDoesNotConsiderMuseumPrivacy() {
        let rooms = [room("r1", "Public", .public), room("r2", "Private", .private)]

        XCTAssertEqual(
            MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: .visitor).map(\.roomID),
            ["r1"]
        )
    }
}
