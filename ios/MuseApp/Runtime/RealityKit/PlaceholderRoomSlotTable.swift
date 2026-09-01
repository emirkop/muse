import simd

enum PlaceholderRoomSlotTable {
    static let variantID = "fixture:placeholder-box"

    static let mountHeight: Float = 1.55
    static let sideEnvelope = SIMD3<Float>(0.62, 0.62, 1)
    static let endEnvelope = SIMD3<Float>(1.6, 1.2, 1)
    static let wallOffset: Float = PlaceholderRoom.wallThickness / 2 + 0.01

    static func build() -> RoomVariantSlotTable {
        var transforms: [SlotAnchor: SlotTransform] = [:]

        let halfWidth = PlaceholderRoom.width / 2
        let halfDepth = PlaceholderRoom.depth / 2

        transforms[SlotAnchor(wall: .focal, positionOnWall: 0)] = SlotTransform(
            position: SIMD3<Float>(0, mountHeight, -halfDepth + wallOffset),
            rotation: simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0)),
            scale: endEnvelope
        )

        transforms[SlotAnchor(wall: .rear, positionOnWall: 0)] = SlotTransform(
            position: SIMD3<Float>(0, mountHeight, halfDepth - wallOffset),
            rotation: simd_quatf(angle: .pi, axis: SIMD3<Float>(0, 1, 0)),
            scale: endEnvelope
        )

        let usableDepth = PlaceholderRoom.depth - 0.6
        let pitch = usableDepth / 13
        for position in 0..<13 {
            let z = halfDepth - 0.3 - pitch * (Float(position) + 0.5)

            transforms[SlotAnchor(wall: .left, positionOnWall: position)] = SlotTransform(
                position: SIMD3<Float>(-halfWidth + wallOffset, mountHeight, z),
                rotation: simd_quatf(angle: .pi / 2, axis: SIMD3<Float>(0, 1, 0)),
                scale: sideEnvelope
            )
            transforms[SlotAnchor(wall: .right, positionOnWall: position)] = SlotTransform(
                position: SIMD3<Float>(halfWidth - wallOffset, mountHeight, z),
                rotation: simd_quatf(angle: -.pi / 2, axis: SIMD3<Float>(0, 1, 0)),
                scale: sideEnvelope
            )
        }

        return RoomVariantSlotTable(
            variantID: variantID,
            photoTransforms: transforms,
            sculptureTransforms: sculptureTransforms()
        )
    }

    static func sculptureTransforms() -> [Int: SlotTransform] {
        let halfWidth = PlaceholderRoom.width / 2
        let halfDepth = PlaceholderRoom.depth / 2
        let inset: Float = 1.2
        let envelope = SIMD3<Float>(0.7, 1.4, 0.7)
        let upright = simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0))

        return [
            0: SlotTransform(
                position: SIMD3<Float>(-halfWidth + inset, 0, -halfDepth + inset),
                rotation: upright,
                scale: envelope
            ),
            1: SlotTransform(
                position: SIMD3<Float>(halfWidth - inset, 0, -halfDepth + inset),
                rotation: upright,
                scale: envelope
            ),
            2: SlotTransform(
                position: SIMD3<Float>(-halfWidth + inset, 0, 0),
                rotation: upright,
                scale: envelope
            )
        ]
    }
}
