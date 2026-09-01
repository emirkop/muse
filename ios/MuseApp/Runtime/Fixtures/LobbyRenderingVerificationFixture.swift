import Foundation

enum LobbyRenderingVerificationFixture {
    static let museumID = "fixture-museum"
    static let styleID = PlaceholderLobbyCardTable.styleID

    static func rooms(count: Int) -> [Room] {
        (0..<max(0, count)).map { index in
            Room(
                id: "fixture-room-\(index)",
                name: "Fixture Room \(index + 1)",
                variantID: PlaceholderRoomSlotTable.variantID,
                privacy: index % 3 == 2 ? .private : .public
            )
        }
    }

    static func makeContent(roomCount: Int, viewerRole: MuseumViewerRole) -> LobbyRuntimeContent? {
        let cards = MuseumLobbyVisibility.visibleCards(rooms: rooms(count: roomCount), viewerRole: viewerRole)
        let table = PlaceholderLobbyCardTable.build(cardCount: cards.count)

        guard case .resolved(let placements) = LobbyCardPlacementResolver.resolve(
            cards: cards,
            styleID: styleID,
            table: table
        ) else {
            return nil
        }

        return LobbyRuntimeContent(
            museumID: museumID,
            styleID: styleID,
            geometry: .verificationFixture,
            viewerRole: viewerRole,
            cardTable: table,
            placements: placements
        )
    }
}
