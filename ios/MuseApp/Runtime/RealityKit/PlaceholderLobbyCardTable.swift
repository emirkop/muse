import simd

enum PlaceholderLobbyCardTable {
    static let styleID = "fixture:placeholder-lobby"

    static let mountHeight: Float = 1.5
    static let envelope = SIMD3<Float>(2.2, 1.4, 1)

    static let columns = 4
    static let columnSpacing: Float = 4
    static let rowSpacing: Float = 4
    static let maximumCards = 16

    static func build(cardCount: Int) -> LobbyCardTable {
        let seated = max(0, min(cardCount, maximumCards))
        let facingEntrance = simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0))

        let spots = (0..<seated).map { index -> SlotTransform in
            let column = index % columns
            let row = index / columns
            let x = (Float(column) - Float(columns - 1) / 2) * columnSpacing
            let z = PlaceholderLobby.depth / 2 - 5.5 - Float(row) * rowSpacing
            return SlotTransform(
                position: SIMD3<Float>(x, mountHeight, z),
                rotation: facingEntrance,
                scale: envelope
            )
        }

        return LobbyCardTable(styleID: styleID, cardSpots: spots)
    }

    struct Provider: LobbyCardTableProviding {
        func cardTable(forStyleID styleID: String, cardCount: Int) async -> LobbyCardTable? {
            guard styleID == PlaceholderLobbyCardTable.styleID else { return nil }
            return PlaceholderLobbyCardTable.build(cardCount: cardCount)
        }
    }
}
