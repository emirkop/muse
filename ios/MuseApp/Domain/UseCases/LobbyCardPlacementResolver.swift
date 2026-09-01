import Foundation
import simd

public struct ResolvedLobbyCardPlacement: Equatable, Sendable {
    public let card: LobbyRoomCard
    public let cardIndex: Int
    public let transform: SlotTransform

    public init(card: LobbyRoomCard, cardIndex: Int, transform: SlotTransform) {
        self.card = card
        self.cardIndex = cardIndex
        self.transform = transform
    }

    public var roomID: String { card.roomID }
}

public enum LobbyCardPlacementFailure: Equatable, Sendable {
    case cardTableUnavailable(styleID: String)
    case styleMismatch(expected: String, received: String)
    case insufficientCardSpots(needed: Int, available: Int)
}

public enum LobbyCardPlacementResolution: Equatable, Sendable {
    case resolved([ResolvedLobbyCardPlacement])
    case unresolvable(LobbyCardPlacementFailure)
}

public enum LobbyCardPlacementResolver {
    public static func resolve(
        cards: [LobbyRoomCard],
        styleID: String,
        table: LobbyCardTable
    ) -> LobbyCardPlacementResolution {
        guard table.styleID == styleID else {
            return .unresolvable(.styleMismatch(expected: styleID, received: table.styleID))
        }
        guard table.capacity >= cards.count else {
            return .unresolvable(.insufficientCardSpots(needed: cards.count, available: table.capacity))
        }

        let placements = cards.enumerated().map { index, card in
            ResolvedLobbyCardPlacement(card: card, cardIndex: index, transform: table.cardSpots[index])
        }
        return .resolved(placements)
    }
}
