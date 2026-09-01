import RealityKit
import simd

enum PlaceholderRoom {
    static let width: Float = 8
    static let depth: Float = 10
    static let height: Float = 3
    static let wallThickness: Float = 0.2

    static let spawnPoint = MuseumCameraSubject(
        position: SIMD3<Float>(0, 0, depth / 2 - 1),
        yaw: 0
    )

    @MainActor
    static func build() -> Entity {
        let room = Entity()
        let material = SimpleMaterial(color: .systemGray, isMetallic: false)

        let floor = makeSlab(
            size: SIMD3<Float>(width, wallThickness, depth),
            position: SIMD3<Float>(0, -wallThickness / 2, 0),
            material: material
        )
        room.addChild(floor)

        let halfWidth = width / 2
        let halfDepth = depth / 2
        let wallCentreY = height / 2

        room.addChild(makeSlab(
            size: SIMD3<Float>(wallThickness, height, depth),
            position: SIMD3<Float>(-halfWidth, wallCentreY, 0),
            material: material
        ))
        room.addChild(makeSlab(
            size: SIMD3<Float>(wallThickness, height, depth),
            position: SIMD3<Float>(halfWidth, wallCentreY, 0),
            material: material
        ))

        room.addChild(makeSlab(
            size: SIMD3<Float>(width, height, wallThickness),
            position: SIMD3<Float>(0, wallCentreY, -halfDepth),
            material: material
        ))
        room.addChild(makeSlab(
            size: SIMD3<Float>(width, height, wallThickness),
            position: SIMD3<Float>(0, wallCentreY, halfDepth),
            material: material
        ))

        return room
    }

    @MainActor
    private static func makeSlab(size: SIMD3<Float>, position: SIMD3<Float>, material: SimpleMaterial) -> ModelEntity {
        let slab = ModelEntity(
            mesh: .generateBox(size: size),
            materials: [material]
        )
        slab.position = position
        slab.collision = CollisionComponent(shapes: [.generateBox(size: size)])
        return slab
    }
}
