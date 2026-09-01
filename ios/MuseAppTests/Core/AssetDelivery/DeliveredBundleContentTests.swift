import Foundation
import RealityKit
import XCTest
import simd
@testable import MuseApp

final class RoomVariantLayoutFileTests: XCTestCase {

    private func layoutJSON(variantID: String = "dev-fixture:room-variant") -> Data {
        BundleFixture.layoutBody(variantID: variantID, version: 1)
    }

    func test_decodesIntoASlotTableAndAnEntryPoint() throws {
        let decoded = try RoomVariantLayoutFile.decode(layoutJSON())

        XCTAssertEqual(decoded.table.variantID, "dev-fixture:room-variant")
        XCTAssertEqual(decoded.table.photoTransforms.count, 4)
        XCTAssertEqual(decoded.table.sculptureTransforms.count, 1)
        XCTAssertEqual(decoded.entry.position, SIMD3<Float>(0, 0, 3.5))
        XCTAssertEqual(decoded.entry.yaw, 0)
    }

    func test_quaternionIsReadAsXYZW() throws {
        let decoded = try RoomVariantLayoutFile.decode(layoutJSON())
        let left = try XCTUnwrap(decoded.table.photoTransforms[SlotAnchor(wall: .left, positionOnWall: 0)])

        let forward = left.rotation.act(SIMD3<Float>(0, 0, 1))
        XCTAssertEqual(forward.x, 1, accuracy: 0.001, "left wall must face +X, into the room")
        XCTAssertEqual(forward.z, 0, accuracy: 0.001)

        let right = try XCTUnwrap(decoded.table.photoTransforms[SlotAnchor(wall: .right, positionOnWall: 0)])
        XCTAssertEqual(right.rotation.act(SIMD3<Float>(0, 0, 1)).x, -1, accuracy: 0.001,
                       "right wall must face -X, into the room")
    }

    func test_missingScaleDefaultsToAUnitEnvelope() throws {
        let json = """
        {"format_version":1,"variant_id":"v","entry":{"position":[0,0,0],"yaw":0},
         "photo_transforms":[{"wall":"focal","position_on_wall":0,"position":[0,1,0]}]}
        """
        let decoded = try RoomVariantLayoutFile.decode(Data(json.utf8))
        let focal = try XCTUnwrap(decoded.table.photoTransforms[SlotAnchor(wall: .focal, positionOnWall: 0)])
        XCTAssertEqual(focal.scale, SIMD3<Float>(1, 1, 1))
        XCTAssertEqual(focal.rotation.vector, simd_quatf(ix: 0, iy: 0, iz: 0, r: 1).vector)
    }

    func test_refusesALayoutWithNoEntryPoint() {
        let json = """
        {"format_version":1,"variant_id":"v",
         "photo_transforms":[{"wall":"focal","position_on_wall":0,"position":[0,1,0]}]}
        """
        XCTAssertThrowsError(try RoomVariantLayoutFile.decode(Data(json.utf8))) { error in
            XCTAssertEqual(error as? RoomVariantLayoutFile.DecodingFailure, .missingEntryPoint)
        }
    }

    func test_refusesAFutureFormatVersion() {
        let json = """
        {"format_version":99,"variant_id":"v","entry":{"position":[0,0,0],"yaw":0},
         "photo_transforms":[{"wall":"focal","position_on_wall":0,"position":[0,1,0]}]}
        """
        XCTAssertThrowsError(try RoomVariantLayoutFile.decode(Data(json.utf8))) { error in
            XCTAssertEqual(error as? RoomVariantLayoutFile.DecodingFailure, .unsupportedFormatVersion(99))
        }
    }

    func test_refusesAnUnknownWall() {
        let json = """
        {"format_version":1,"variant_id":"v","entry":{"position":[0,0,0],"yaw":0},
         "photo_transforms":[{"wall":"ceiling","position_on_wall":0,"position":[0,1,0]}]}
        """
        XCTAssertThrowsError(try RoomVariantLayoutFile.decode(Data(json.utf8))) { error in
            XCTAssertEqual(error as? RoomVariantLayoutFile.DecodingFailure, .unknownWall("ceiling"))
        }
    }

    func test_refusesALayoutWithNoTransforms() {
        let json = """
        {"format_version":1,"variant_id":"v","entry":{"position":[0,0,0],"yaw":0},"photo_transforms":[]}
        """
        XCTAssertThrowsError(try RoomVariantLayoutFile.decode(Data(json.utf8))) { error in
            XCTAssertEqual(error as? RoomVariantLayoutFile.DecodingFailure, .noPhotoTransforms)
        }
    }

    func test_aDeliveredTableResolvesPlacementsThroughThePlacementEngine() throws {
        let decoded = try RoomVariantLayoutFile.decode(layoutJSON())
        let room = Room(
            id: "r", name: "Delivered", variantID: "dev-fixture:room-variant", privacy: .private,
            photoSlots: (0..<4).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") }
        )

        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: decoded.table) else {
            return XCTFail("a delivered table must resolve a 4-photo Room")
        }
        XCTAssertEqual(placements.count, 4)
        XCTAssertEqual(placements.map(\.anchor.wall), [.focal, .left, .right, .rear])
    }

    func test_aTableForAnotherVariantIsRefusedByTheResolver() throws {
        let decoded = try RoomVariantLayoutFile.decode(layoutJSON(variantID: "some-other-variant"))
        let room = Room(id: "r", name: "R", variantID: "dev-fixture:room-variant", privacy: .private,
                        photoSlots: [PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a", caption: "")])
        guard case .unresolvable(.variantMismatch) = RoomPlacementResolver.resolve(room: room, slotTable: decoded.table) else {
            return XCTFail("expected a variantMismatch refusal")
        }
    }
}

// MARK: - The committed development fixture

final class DevFixtureBundleTests: XCTestCase {

    private func fixtureURL(version: Int, file: String) -> URL? {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        let url = root
            .appendingPathComponent("assets/dev_fixtures/bundles/dev_fixture_room_variant/v\(version)")
            .appendingPathComponent(file)
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
    }

    func test_theCommittedFixtureLayoutSatisfiesTheContract() throws {
        guard let url = fixtureURL(version: 1, file: "layout.json") else {
            throw XCTSkip("assets/dev_fixtures is not reachable from this test host")
        }
        let decoded = try RoomVariantLayoutFile.decode(contentsOf: url)

        XCTAssertEqual(decoded.table.variantID, "dev-fixture:room-variant",
                       "the fixture's variant id must be unmistakably a fixture, never catalog-shaped")
        XCTAssertTrue(decoded.table.supportsFullRoom,
                      "the fixture must supply all 28 anchors (1 focal + 13 left + 13 right + 1 rear)")
        XCTAssertEqual(decoded.table.photoTransforms.count, 28)
        XCTAssertEqual(decoded.table.sculptureTransforms.count, 3)

        for count in [1, 2, 14, 27, 28] {
            let room = Room(id: "r", name: "R", variantID: "dev-fixture:room-variant", privacy: .private,
                            photoSlots: (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") })
            guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: decoded.table) else {
                return XCTFail("the fixture must resolve a \(count)-photo Room")
            }
            XCTAssertEqual(placements.count, count)
        }
    }

    func test_fixtureV2DiffersFromV1() throws {
        guard let first = fixtureURL(version: 1, file: "geometry.usda"),
              let second = fixtureURL(version: 2, file: "geometry.usda") else {
            throw XCTSkip("assets/dev_fixtures is not reachable from this test host")
        }
        XCTAssertNotEqual(try Data(contentsOf: first), try Data(contentsOf: second),
                          "v1 and v2 must differ, or a version-bump test proves nothing")

        let layoutOne = try RoomVariantLayoutFile.decode(contentsOf: try XCTUnwrap(fixtureURL(version: 1, file: "layout.json")))
        let layoutTwo = try RoomVariantLayoutFile.decode(contentsOf: try XCTUnwrap(fixtureURL(version: 2, file: "layout.json")))
        let anchor = SlotAnchor(wall: .left, positionOnWall: 0)
        XCTAssertNotEqual(layoutOne.table.photoTransforms[anchor]?.position,
                          layoutTwo.table.photoTransforms[anchor]?.position,
                          "re-authoring the geometry must move the layout with it")
    }

    @MainActor
    func test_theCommittedFixtureGeometryLoadsIntoRealityKit() throws {
        guard let url = fixtureURL(version: 1, file: "geometry.usda") else {
            throw XCTSkip("assets/dev_fixtures is not reachable from this test host")
        }

        let entity = try Entity.load(contentsOf: url)
        entity.generateCollisionShapes(recursive: true)

        var meshes = 0
        var collidable = 0
        func walk(_ entity: Entity) {
            if entity.components[ModelComponent.self] != nil { meshes += 1 }
            if entity.components[CollisionComponent.self] != nil { collidable += 1 }
            entity.children.forEach(walk)
        }
        walk(entity)

        XCTAssertEqual(meshes, 5, "expected floor + four walls as separate meshes")
        XCTAssertEqual(collidable, 5, "a delivered space must be solid, or the viewer walks through it")

        let bounds = entity.visualBounds(relativeTo: nil)
        XCTAssertEqual(bounds.extents.x, 7.15, accuracy: 0.05)
        XCTAssertEqual(bounds.extents.y, 3.15, accuracy: 0.05)
        XCTAssertEqual(bounds.extents.z, 9.15, accuracy: 0.05)
    }
}

// MARK: - Runtime content

final class DeliveredRoomGeometryTests: XCTestCase {

    func test_aDeliveredBundleAndTheFixtureAreDistinctGeometryCases() throws {
        let delivered = RoomRuntimeContent.Geometry.variantBundle(
            RoomVariantGeometry(
                variantID: "dev-fixture:room-variant",
                identity: AssetBundleIdentity(bundleID: "b", version: 3),
                format: "usda",
                fileURL: URL(fileURLWithPath: "/tmp/geometry.usda"),
                entry: MuseumCameraSubject(position: SIMD3<Float>(0, 0, 3.5), yaw: 0)
            )
        )
        XCTAssertNotEqual(delivered, .verificationFixture)

        guard case .variantBundle(let variant) = delivered else {
            return XCTFail("expected a variantBundle")
        }
        XCTAssertEqual(variant.identity.version, 3)
        XCTAssertEqual(variant.entry.position.z, 3.5)
    }

    func test_withNothingPublishedARealRoomStillReportsItsDesignUnavailable() async {
        let source = StubManifestSource()
        let bundles = AssetBundleDeliveryService(
            manifests: source,
            downloader: URLSessionAssetBundleDownloader(session: RangeAwareOriginProtocol.session()),
            cache: AssetBundleCache(
                store: AssetBundleStore(root: URL(fileURLWithPath: NSTemporaryDirectory())
                    .appendingPathComponent("MuseUnpublished-\(UUID().uuidString)")),
                retention: ActiveBundleRegistry()
            )
        )
        let variants = StubVariantLookup(variants: [
            "style_modern_variant_Hall": RoomVariant(
                id: "style_modern_variant_Hall", styleID: "style_modern", displayName: "Hall",
                assetBundle: AssetBundleRef(id: "bundle_style_modern_Hall", version: 1)
            )
        ])
        let layouts = DeliveredVariantLayoutProvider(bundles: bundles, variants: variants, accessToken: { "t" })

        let table = await layouts.slotTable(forVariantID: "style_modern_variant_Hall")
        XCTAssertNil(table, "no bundle is published, so there is no table — the truthful answer")
        let design = await layouts.design(forVariantID: "style_modern_variant_Hall", progress: { _ in })
        guard case .unavailable(let reason) = design else { return XCTFail("expected unavailable, got \(design)") }
        XCTAssertEqual(reason, .notPublished, "the honest reason for every production Variant today")
    }

    func test_publishingABundleMakesTheProvidersAnswerYesWithNoCodeChange() async throws {
        let source = StubManifestSource()
        let store = AssetBundleStore(root: URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("MusePublished-\(UUID().uuidString)"))
        defer { store.removeAll() }

        URLProtocol.registerClass(RangeAwareOriginProtocol.self)
        defer {
            RangeAwareOriginProtocol.reset()
            URLProtocol.unregisterClass(RangeAwareOriginProtocol.self)
        }
        RangeAwareOriginProtocol.reset()

        let bundles = AssetBundleDeliveryService(
            manifests: source,
            downloader: URLSessionAssetBundleDownloader(session: RangeAwareOriginProtocol.session()),
            cache: AssetBundleCache(store: store, retention: ActiveBundleRegistry())
        )
        let variants = StubVariantLookup(variants: [
            "dev-fixture:room-variant": RoomVariant(
                id: "dev-fixture:room-variant", styleID: "n/a", displayName: "Fixture",
                assetBundle: AssetBundleRef(id: "dev_fixture_room_variant", version: 1)
            )
        ])
        let layouts = DeliveredVariantLayoutProvider(bundles: bundles, variants: variants, accessToken: { "t" })

        let before = await layouts.design(forVariantID: "dev-fixture:room-variant", progress: { _ in })
        guard case .unavailable(.notPublished) = before else { return XCTFail("expected notPublished, got \(before)") }

        BundleFixture.publish(bundleID: "dev_fixture_room_variant", version: 1, into: source)

        let recorder = StateRecorder()
        let resolution = await layouts.design(forVariantID: "dev-fixture:room-variant", progress: { state in
            recorder.record(Self.deliveryState(from: state))
        })
        guard case .available(let design) = resolution, case .variantBundle(let variant) = design.geometry else {
            return XCTFail("expected delivered geometry, got \(resolution)")
        }
        XCTAssertEqual(design.slotTable.variantID, "dev-fixture:room-variant", "table and geometry from one resolution")
        let observed = recorder.states
        XCTAssertTrue(observed.contains { if case .geometryReady = $0 { return true } else { return false } },
                      "geometry-first must be observable at the design port: \(observed)")

        let table = await layouts.slotTable(forVariantID: "dev-fixture:room-variant")
        XCTAssertNotNil(table, "a published bundle must produce a slot table")
        XCTAssertEqual(table?.variantID, "dev-fixture:room-variant")
        let hitRecorder = StateRecorder()
        _ = await layouts.design(forVariantID: "dev-fixture:room-variant", progress: { hitRecorder.record(Self.deliveryState(from: $0)) })
        XCTAssertFalse(hitRecorder.states.contains { if case .downloading = $0 { return true } else { return false } },
                       "a cache hit must show no download state: \(hitRecorder.states)")
        XCTAssertEqual(variant.identity.version, 1)
        XCTAssertTrue(FileManager.default.fileExists(atPath: variant.fileURL.path),
                      "the geometry must be a local file the runtime can load")
        XCTAssertEqual(variant.entry.position.z, 3.5, accuracy: 0.001)
    }
}

extension DeliveredRoomGeometryTests {
    static func deliveryState(from state: RoomDesignLoadState) -> AssetBundleDeliveryState {
        switch state {
        case .checking: return .notStarted
        case .downloading(let f): return .downloading(fractionComplete: f)
        case .geometryReady(let f): return .geometryReady(fractionComplete: f)
        case .ready: return .installed(InstalledAssetBundle(
            identity: AssetBundleIdentity(bundleID: "-", version: 0), kind: .roomVariant, format: "-", files: [:], roles: [:]))
        }
    }
}

struct StubVariantLookup: RoomVariantCatalogLookup {
    let variants: [String: RoomVariant]

    func variant(accessToken: String, variantID: String) async throws -> RoomVariant? {
        variants[variantID]
    }
}

// MARK: -: a Collection Design's preview through the real path

final class CollectionDesignPreviewDeliveryTests: XCTestCase {
    private var root: URL!
    private var store: AssetBundleStore!
    private var source: StubManifestSource!
    private var registry: ActiveBundleRegistry!

    override func setUp() {
        super.setUp()
        URLProtocol.registerClass(RangeAwareOriginProtocol.self)
        RangeAwareOriginProtocol.reset()
        root = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("MuseCollectionDesignPreview-\(UUID().uuidString)", isDirectory: true)
        store = AssetBundleStore(root: root)
        source = StubManifestSource()
        registry = ActiveBundleRegistry()
    }

    override func tearDown() {
        store?.removeAll()
        RangeAwareOriginProtocol.reset()
        URLProtocol.unregisterClass(RangeAwareOriginProtocol.self)
        super.tearDown()
    }

    private func publishDesignBundle(bundleID: String, version: Int) -> AssetBundleManifest {
        let geometry = Data("#usda 1.0\n(defaultPrim = \"Design\")\ndef Xform \"Design\" {}\n".utf8)
        let url = URL(string: "https://assets.example/bundles/\(bundleID)/v\(version)/geometry")!
        RangeAwareOriginProtocol.install(.init(body: geometry), at: url)

        let manifest = AssetBundleManifest(
            identity: AssetBundleIdentity(bundleID: bundleID, version: version),
            kind: .collectionDesign,
            format: "usda",
            minAppVersion: 1,
            files: [
                AssetBundleFile(
                    assetID: "geometry", role: .geometry, url: url,
                    contentType: "model/vnd.usda+ascii",
                    byteSize: Int64(geometry.count), checksumSHA256: BundleFixture.sha256Hex(geometry)
                )
            ]
        )
        source.publish(manifest)
        return manifest
    }

    private func makeProvider() -> DeliveredPreviewAssetProvider {
        let service = AssetBundleDeliveryService(
            manifests: source,
            downloader: URLSessionAssetBundleDownloader(session: RangeAwareOriginProtocol.session()),
            cache: AssetBundleCache(store: store, policy: .developmentDefault, retention: registry)
        )
        return DeliveredPreviewAssetProvider(bundles: service, accessToken: { "token" })
    }

    private func fixtureDesign(bundleID: String, version: Int) -> CollectionDesign {
        CollectionDesign(
            id: "dev-fixture:collection-design",
            displayName: "Development Fixture (not a real design)",
            categoryID: nil,
            isDevelopmentFixture: true,
            assetBundle: AssetBundleRef(id: bundleID, version: version)
        )
    }

    func test_devFixtureDesignPreviewLoadsThroughTheRealDeliveryPath() async throws {
        publishDesignBundle(bundleID: "dev_fixture_collection_design", version: 1)
        let design = fixtureDesign(bundleID: "dev_fixture_collection_design", version: 1)

        let availability = await makeProvider().availability(for: design.previewSubject)

        XCTAssertEqual(availability, .ready, "the fixture Design's bundle must resolve through real delivery")
        let installed = store.versionDirectory(
            AssetBundleIdentity(bundleID: "dev_fixture_collection_design", version: 1)
        )
        XCTAssertTrue(
            FileManager.default.fileExists(atPath: installed.appendingPathComponent("geometry").path),
            "the geometry file should be installed at \(installed.path)"
        )
    }

    func test_aGeometryOnlyBundleIsSufficient() async throws {
        let manifest = publishDesignBundle(bundleID: "design_geometry_only", version: 1)

        XCTAssertEqual(manifest.files.count, 1)
        XCTAssertEqual(manifest.files[0].role, .geometry)
        XCTAssertFalse(
            manifest.files.contains(where: { $0.role == .layout }),
            "a Collection Design fixture must carry no layout — / are open"
        )

        let availability = await makeProvider().availability(
            for: fixtureDesign(bundleID: "design_geometry_only", version: 1).previewSubject
        )
        XCTAssertEqual(availability, .ready)
    }

    func test_repointingTheDesignAtANewBundleVersionFetchesTheNewOne() async throws {
        publishDesignBundle(bundleID: "design_repoint", version: 1)
        let provider = makeProvider()

        let first = await provider.availability(
            for: fixtureDesign(bundleID: "design_repoint", version: 1).previewSubject
        )
        XCTAssertEqual(first, .ready)

        publishDesignBundle(bundleID: "design_repoint", version: 2)

        let second = await provider.availability(
            for: fixtureDesign(bundleID: "design_repoint", version: 2).previewSubject
        )
        XCTAssertEqual(second, .ready, "the newly published version must resolve")

        let v2 = store.versionDirectory(AssetBundleIdentity(bundleID: "design_repoint", version: 2))
        XCTAssertTrue(
            FileManager.default.fileExists(atPath: v2.appendingPathComponent("geometry").path),
            "v2's geometry should be installed"
        )
    }
}

// MARK: -: the committed fixture's tier table

final class CollectionDesignLayoutFileTests: XCTestCase {

    private func fixtureLayoutURL() throws -> URL {
        var root = URL(fileURLWithPath: #filePath)
        for _ in 0..<5 { root.deleteLastPathComponent() }
        return root
            .appendingPathComponent("assets/dev_fixtures/bundles")
            .appendingPathComponent("dev_fixture_collection_design/v1/layout.json")
    }

    func test_theCommittedFixtureLayoutDecodesIntoACoherentTierTable() throws {
        let url = try fixtureLayoutURL()
        guard FileManager.default.fileExists(atPath: url.path) else {
            return XCTFail("the committed fixture layout is missing at \(url.path)")
        }

        let table = try CollectionDesignLayoutFile.decode(contentsOf: url)

        XCTAssertEqual(table.designID, "dev-fixture:collection-design")
        XCTAssertNil(table.rejection, "the generator must emit a coherent table")

        XCTAssertEqual(table.capacities.cumulative, [4, 10, 18])
        XCTAssertEqual(table.tiers.map(\.itemTransforms.count), [4, 6, 8])
        XCTAssertEqual(table.highestTier, CollectionTier(3))

        XCTAssertNil(table.tiers[0].additionalGeometry)
        XCTAssertEqual(table.tiers[1].additionalGeometry?.id, "dev_fixture_collection_design_tier2")
        XCTAssertEqual(table.tiers[2].additionalGeometry?.id, "dev_fixture_collection_design_tier3")

        XCTAssertNotEqual(table.entry.position, .zero)
    }

    func test_aLayoutWithoutAnEntryPointIsRefused() {
        let json = """
        {"format_version": 1, "design_id": "d",
         "tiers": [{"tier": 1, "cumulative_capacity": 1,
                    "item_transforms": [{"slot_index": 0, "position": [0,0,0]}]}]}
        """
        XCTAssertThrowsError(try CollectionDesignLayoutFile.decode(Data(json.utf8))) { error in
            XCTAssertEqual(error as? CollectionDesignLayoutFile.DecodingFailure, .missingEntryPoint)
        }
    }

    func test_anUnsupportedFormatVersionIsRefused() {
        let json = """
        {"format_version": 99, "design_id": "d", "entry": {"position": [0,0,0]}, "tiers": []}
        """
        XCTAssertThrowsError(try CollectionDesignLayoutFile.decode(Data(json.utf8))) { error in
            XCTAssertEqual(error as? CollectionDesignLayoutFile.DecodingFailure, .unsupportedFormatVersion(99))
        }
    }

    func test_anIncoherentTableIsRefusedAtDecode() {
        let json = """
        {"format_version": 1, "design_id": "d", "entry": {"position": [0,0,3]},
         "tiers": [{"tier": 1, "cumulative_capacity": 4,
                    "item_transforms": [{"slot_index": 0, "position": [0,1,0]}]}]}
        """
        XCTAssertThrowsError(try CollectionDesignLayoutFile.decode(Data(json.utf8))) { error in
            XCTAssertEqual(
                error as? CollectionDesignLayoutFile.DecodingFailure,
                .incoherentTierTable(.slotCountDoesNotMatchAddedCapacity(tier: 1))
            )
        }
    }
}
