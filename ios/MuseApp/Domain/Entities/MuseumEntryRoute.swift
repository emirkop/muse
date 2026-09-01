import Foundation

public enum MuseumEntryRoute: Equatable, Sendable {
    case noVisibleRooms
    case enterRoomDirectly(roomID: String)
    case lobby(cards: [LobbyRoomCard])
}

public enum MuseumLobbyRouting {
    public static func route(forVisible cards: [LobbyRoomCard]) -> MuseumEntryRoute {
        switch cards.count {
        case 0:
            return .noVisibleRooms
        case 1:
            return .enterRoomDirectly(roomID: cards[0].roomID)
        default:
            return .lobby(cards: cards)
        }
    }
}
