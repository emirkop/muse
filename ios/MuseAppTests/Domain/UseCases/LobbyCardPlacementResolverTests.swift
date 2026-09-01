import simd
import XCTest
@testable import MuseApp

final class LobbyCardPlacementResolverTests: XCTestCase {
    private let styleID = "style_modern"

    private func cards(_ count: Int) -> [LobbyRoomCard] {
        (0..<count).map { LobbyRoomCard(roomID: "r\($0)", name: "Room \($0)", isMarkedPrivate: false) }
    }

    private func table(styleID: String, spots: Int) -> LobbyCardTable {
        LobbyCardTable(
            styleID: styleID,
            cardSpots: (0..<spots).map {
                SlotTransform(position: SIMD3<Float>(Float($0), 1.5, 0))
            }
        )
    }

    func test_resolvesEachCardToTheSpotAtItsOwnIndex() {
        let result = LobbyCardPlacementResolver.resolve(
            cards: cards(3),
            styleID: styleID,
            table: table(styleID: styleID, spots: 4)
        )

        guard case .resolved(let placements) = result else { return XCTFail("expected resolution") }
        XCTAssertEqual(placements.map(\.roomID), ["r0", "r1", "r2"])
        XCTAssertEqual(placements.map(\.cardIndex), [0, 1, 2])
        XCTAssertEqual(placements.map { $0.transform.position.x }, [0, 1, 2])
    }

    func test_carriesTheCardThrough_soSignageNeedsNoSecondLookup() {
        let card = LobbyRoomCard(roomID: "r0", name: "Studio", isMarkedPrivate: true)

        let result = LobbyCardPlacementResolver.resolve(
            cards: [card],
            styleID: styleID,
            table: table(styleID: styleID, spots: 1)
        )

        guard case .resolved(let placements) = result else { return XCTFail("expected resolution") }
        XCTAssertEqual(placements[0].card, card)
    }

    func test_sparerTableThanNeeded_isRefusedNotTruncated() {
        let result = LobbyCardPlacementResolver.resolve(
            cards: cards(5),
            styleID: styleID,
            table: table(styleID: styleID, spots: 3)
        )

        XCTAssertEqual(result, .unresolvable(.insufficientCardSpots(needed: 5, available: 3)))
    }

    func test_anotherStylesTable_isRefused() {
        let result = LobbyCardPlacementResolver.resolve(
            cards: cards(2),
            styleID: styleID,
            table: table(styleID: "style_brutalist", spots: 8)
        )

        XCTAssertEqual(
            result,
            .unresolvable(.styleMismatch(expected: styleID, received: "style_brutalist")),
            "another Lobby's coordinates would put every card in the wrong place"
        )
    }

    func test_styleMismatchIsCheckedBeforeCapacity() {
        let result = LobbyCardPlacementResolver.resolve(
            cards: cards(4),
            styleID: styleID,
            table: table(styleID: "other", spots: 1)
        )

        XCTAssertEqual(result, .unresolvable(.styleMismatch(expected: styleID, received: "other")))
    }

    func test_noCards_resolvesToNoPlacements() {
        let result = LobbyCardPlacementResolver.resolve(
            cards: [],
            styleID: styleID,
            table: table(styleID: styleID, spots: 4)
        )

        XCTAssertEqual(result, .resolved([]))
    }

    func test_productionProviderHasNoTable() async {
        let table = await UnavailableLobbyCardTableProvider().cardTable(forStyleID: styleID, cardCount: 3)

        XCTAssertNil(table, " authored no Lobby, so no Style has a card table")
    }

    func test_productionGeometryProviderHasNoLobby() async {
        let geometry = await UnavailableLobbyGeometryProvider().lobbyGeometry(forStyleID: styleID)

        XCTAssertNil(geometry, "`assets/blender/museum/` is empty — no Lobby space exists to render")
    }
}
