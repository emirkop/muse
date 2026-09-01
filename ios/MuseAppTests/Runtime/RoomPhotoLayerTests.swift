import RealityKit
import simd
import XCTest
@testable import MuseApp

@MainActor
final class RoomPhotoLayerTests: XCTestCase {

    private var arView: ARView!
    private var anchor: AnchorEntity!

    override func setUp() {
        arView = ARView(frame: CGRect(x: 0, y: 0, width: 320, height: 480), cameraMode: .nonAR, automaticallyConfigureSession: false)
        anchor = AnchorEntity(world: .zero)
        arView.scene.addAnchor(anchor)
    }

    override func tearDown() {
        arView.scene.anchors.removeAll()
        arView = nil
        anchor = nil
    }

    private func placements(_ count: Int) -> [ResolvedPhotoPlacement] {
        let room = Room(id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
                        photoSlots: (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "cap \($0)") })
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: PlaceholderRoomSlotTable.build()) else {
            XCTFail("fixture table must resolve \(count)")
            return []
        }
        return placements
    }

    private func image(width: Int, height: Int) -> DecodedPhotoImage {
        try! PhotoTextureDecoder.decode(PhotoTextureDecoderTests.jpeg(width: width, height: height), maxLongEdge: 256)
    }

    // MARK: - Mounting

    func test_mount_createsOnePlanePerPlacement_atItsResolvedTransform() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let placements = placements(28)

        layer.mount(placements)

        XCTAssertEqual(layer.root.children.count, 28)
        XCTAssertEqual(layer.mountedSlotIndices, Set(0..<28))
        for placement in placements {
            let entity = layer.root.children.first { $0.name == "photo-\(placement.photoAssetID)" }!
            XCTAssertEqual(entity.position, placement.transform.position, "slot \(placement.slotIndex) position")
            XCTAssertEqual(entity.orientation.vector, placement.transform.rotation.vector, "slot \(placement.slotIndex) rotation")
            XCTAssertEqual(entity.scale, .one, "the envelope is consumed by the plane size, never applied as entity scale")
        }
        XCTAssertTrue(layer.texturedSlotIndices.isEmpty, "geometry first — no texture yet")
    }

    func test_mount_forOneTwoAndFourteen_placesExactlyThoseSlots() {
        for count in [1, 2, 14, 27] {
            let layer = RoomPhotoLayer()
            anchor.addChild(layer.root)
            layer.mount(placements(count))
            XCTAssertEqual(layer.root.children.count, count, "count \(count)")
            layer.tearDown()
            layer.root.removeFromParent()
        }
    }

    func test_storedDimensions_refitThePlaneBeforeAnyTexture() {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(2))

        let before = layer.planeBounds(forSlot: 1)!
        XCTAssertGreaterThan(before.x, before.y, "provisional plane is landscape")

        layer.setStoredDimensions(pixelWidth: 2048, pixelHeight: 3072, forAsset: "a1", generation: gen)

        let after = layer.planeBounds(forSlot: 1)!
        XCTAssertLessThan(after.x, after.y, "portrait dimensions must produce a portrait plane")
        XCTAssertEqual(after.x / after.y, 2048.0 / 3072.0, accuracy: 0.01)
        let envelope = placements(2)[1].transform.scale
        XCTAssertLessThanOrEqual(after.y, envelope.y + 0.001)
        XCTAssertLessThanOrEqual(after.x, envelope.x + 0.001)
    }

    // MARK: - Texturing

    func test_applyingTextures_outOfOrder_landsOnTheRightSlots() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(5))

        for slot in [3, 0, 4, 1, 2] {
            let landscape = slot % 2 == 0
            await layer.apply(image(width: landscape ? 300 : 200, height: landscape ? 200 : 300), forAsset: "a\(slot)", generation: gen)
        }

        XCTAssertEqual(layer.texturedSlotIndices, Set(0..<5))
        for slot in 0..<5 {
            let bounds = layer.planeBounds(forSlot: slot)!
            if slot % 2 == 0 {
                XCTAssertGreaterThan(bounds.x, bounds.y, "slot \(slot) must be landscape")
            } else {
                XCTAssertLessThan(bounds.x, bounds.y, "slot \(slot) must be portrait")
            }
            let entity = layer.root.children.first { $0.name == "photo-a\(slot)" } as! ModelEntity
            let material = entity.model!.materials.first as! PhysicallyBasedMaterial
            XCTAssertNotNil(material.baseColor.texture, "slot \(slot) must carry its texture")
        }
    }

    func test_mixedAspectRatios_allFitTheirEnvelopes() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let placements = placements(6)
        let gen = layer.mount(placements)

        let shapes = [(300, 200), (200, 300), (250, 250), (300, 100), (100, 300), (300, 200)]
        for (slot, shape) in shapes.enumerated() {
            await layer.apply(image(width: shape.0, height: shape.1), forAsset: "a\(slot)", generation: gen)
        }

        for (slot, shape) in shapes.enumerated() {
            let bounds = layer.planeBounds(forSlot: slot)!
            let envelope = placements[slot].transform.scale
            XCTAssertEqual(bounds.x / bounds.y, Float(shape.0) / Float(shape.1), accuracy: 0.02, "slot \(slot) aspect")
            XCTAssertLessThanOrEqual(bounds.x, envelope.x + 0.001, "slot \(slot) width within envelope")
            XCTAssertLessThanOrEqual(bounds.y, envelope.y + 0.001, "slot \(slot) height within envelope")
        }
    }

    // MARK: - Stale results

    func test_textureForAnOlderGeneration_isIgnored() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let firstGen = layer.mount(placements(3))

        layer.tearDown()
        XCTAssertEqual(layer.root.children.count, 0)

        await layer.apply(image(width: 300, height: 200), forAsset: "a\(0)", generation: firstGen)
        layer.setStoredDimensions(pixelWidth: 1, pixelHeight: 2, forAsset: "a0", generation: firstGen)

        XCTAssertEqual(layer.root.children.count, 0, "a stale texture must not resurrect a plane")
        XCTAssertTrue(layer.texturedSlotIndices.isEmpty)
    }

    func test_remounting_dropsResultsForThePreviousRoom() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let firstGen = layer.mount(placements(4))
        let secondGen = layer.mount(placements(2))
        XCTAssertNotEqual(firstGen, secondGen)
        XCTAssertEqual(layer.root.children.count, 2)

        await layer.apply(image(width: 300, height: 200), forAsset: "a\(3)", generation: firstGen)
        await layer.apply(image(width: 300, height: 200), forAsset: "a\(0)", generation: firstGen)

        XCTAssertTrue(layer.texturedSlotIndices.isEmpty, "nothing from the first Room may land on the second")
        XCTAssertEqual(layer.root.children.count, 2)
    }

    // MARK: - Failure isolation

    func test_aFailedSlot_keepsItsPlaceholderPlane_andOthersTexture() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(3))

        layer.markFailed(assetID: "a1", generation: gen)
        await layer.apply(image(width: 300, height: 200), forAsset: "a\(0)", generation: gen)
        await layer.apply(image(width: 300, height: 200), forAsset: "a\(2)", generation: gen)

        XCTAssertEqual(layer.root.children.count, 3, "the failed slot's plane stays — `02`: rest of Room unaffected")
        XCTAssertEqual(layer.texturedSlotIndices, [0, 2])
    }

    // MARK: - Lifecycle

    func test_tearDown_removesEveryPlane_andReleasesMaterials() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let gen = layer.mount(placements(28))
        for slot in 0..<28 {
            await layer.apply(image(width: 300, height: 200), forAsset: "a\(slot)", generation: gen)
        }
        XCTAssertEqual(layer.texturedSlotIndices.count, 28)

        layer.tearDown()

        XCTAssertEqual(layer.root.children.count, 0)
        XCTAssertTrue(layer.mountedSlotIndices.isEmpty)
        XCTAssertTrue(layer.texturedSlotIndices.isEmpty)
    }

    func test_repeatedMountAndTearDown_leavesNothingBehind() async {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let img = image(width: 300, height: 200)

        for cycle in 0..<25 {
            let gen = layer.mount(placements(28))
            for slot in stride(from: 0, to: 28, by: 4) {
                await layer.apply(img, forAsset: "a\(slot)", generation: gen)
            }
            layer.tearDown()
            XCTAssertEqual(layer.root.children.count, 0, "cycle \(cycle)")
            XCTAssertTrue(layer.mountedSlotIndices.isEmpty, "cycle \(cycle)")
        }
        XCTAssertEqual(layer.generation, 25)
    }

    // MARK: - Measurement

    func test_measure_28TexturesResidentMemory() async throws {
        let layer = RoomPhotoLayer()
        anchor.addChild(layer.root)
        let edge = RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: 28)
        let gen = layer.mount(placements(28))
        let images = (0..<28).map { slot -> DecodedPhotoImage in
            let dims = RoomRenderingVerificationFixture.storedDimensions(forSlot: slot)
            return try! PhotoTextureDecoder.decode(PhotoTextureDecoderTests.jpeg(width: dims.width, height: dims.height), maxLongEdge: edge)
        }

        let before = ResidentMemory.bytes()
        let started = Date()
        for (slot, image) in images.enumerated() {
            await layer.apply(image, forAsset: "a\(slot)", generation: gen)
        }
        let elapsed = Date().timeIntervalSince(started)
        let after = ResidentMemory.bytes()

        XCTAssertEqual(layer.texturedSlotIndices.count, 28)
        let growthMB = Double(after - before) / 1_048_576
        let budgetMB = Double(RoomPhotoTexturePolicy.textureBudgetBytes) / 1_048_576
        print("[measurement] 28 textures @ \(edge)px: upload \(String(format: "%.2f", elapsed))s, resident +\(String(format: "%.1f", growthMB)) MB (policy ceiling \(Int(budgetMB)) MB)")
        XCTAssertLessThan(growthMB, budgetMB * 3, "texture footprint far above the policy's expectation")
    }
}
