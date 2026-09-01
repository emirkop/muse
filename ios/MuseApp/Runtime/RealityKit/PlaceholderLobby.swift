import RealityKit
import simd

enum PlaceholderLobby {
    static let width: Float = 18
    static let depth: Float = 18
    static let height: Float = 5
    static let wallThickness: Float = 0.25

    static let spawnPoint = MuseumCameraSubject(
        position: SIMD3<Float>(0, 0, depth / 2 - 1.5),
        yaw: 0
    )

    @MainActor
    static func build() -> Entity {
        let lobby = Entity()
        let material = SimpleMaterial(color: .darkGray, isMetallic: false)

        lobby.addChild(makeSlab(
            size: SIMD3<Float>(width, wallThickness, depth),
            position: SIMD3<Float>(0, -wallThickness / 2, 0),
            material: material
        ))

        let halfWidth = width / 2
        let halfDepth = depth / 2
        let wallCentreY = height / 2

        lobby.addChild(makeSlab(
            size: SIMD3<Float>(wallThickness, height, depth),
            position: SIMD3<Float>(-halfWidth, wallCentreY, 0),
            material: material
        ))
        lobby.addChild(makeSlab(
            size: SIMD3<Float>(wallThickness, height, depth),
            position: SIMD3<Float>(halfWidth, wallCentreY, 0),
            material: material
        ))
        lobby.addChild(makeSlab(
            size: SIMD3<Float>(width, height, wallThickness),
            position: SIMD3<Float>(0, wallCentreY, -halfDepth),
            material: material
        ))
        lobby.addChild(makeSlab(
            size: SIMD3<Float>(width, height, wallThickness),
            position: SIMD3<Float>(0, wallCentreY, halfDepth),
            material: material
        ))

        return lobby
    }

    @MainActor
    private static func makeSlab(size: SIMD3<Float>, position: SIMD3<Float>, material: SimpleMaterial) -> ModelEntity {
        let slab = ModelEntity(mesh: .generateBox(size: size), materials: [material])
        slab.position = position
        slab.collision = CollisionComponent(shapes: [.generateBox(size: size)])
        return slab
    }
}
