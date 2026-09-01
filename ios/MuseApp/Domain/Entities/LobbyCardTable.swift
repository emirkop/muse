import Foundation
import simd

public struct LobbyCardTable: Equatable, Sendable {
    public let styleID: String
    public let cardSpots: [SlotTransform]

    public init(styleID: String, cardSpots: [SlotTransform]) {
        self.styleID = styleID
        self.cardSpots = cardSpots
    }

    public var capacity: Int { cardSpots.count }
}

public protocol LobbyCardTableProviding: Sendable {
    func cardTable(forStyleID styleID: String, cardCount: Int) async -> LobbyCardTable?
}

public struct UnavailableLobbyCardTableProvider: LobbyCardTableProviding {
    public init() {}

    public func cardTable(forStyleID styleID: String, cardCount: Int) async -> LobbyCardTable? {
        nil
    }
}
