import CryptoKit
import Foundation
import XCTest
@testable import MuseApp

private func requireProfiling() throws {
    if ProcessInfo.processInfo.environment["MUSE_PROFILE"] == nil {
        throw XCTSkip("MUSE_PROFILE not set — skipping profiling runs")
    }
}

private func meanMilliseconds(iterations: Int, _ body: () throws -> Void) rethrows -> Double {
    let start = Date()
    for _ in 0..<iterations { try body() }
    return Date().timeIntervalSince(start) * 1000 / Double(iterations)
}

// MARK: - assigned measurement

@MainActor
final class CacheProfileTests: XCTestCase {
    private var root: URL!
    private var store: AssetBundleStore!
    private var registry: ActiveBundleRegistry!

    override func setUp() async throws {
        try await super.setUp()
        root = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("Cache-\(UUID().uuidString)", isDirectory: true)
        store = AssetBundleStore(root: root)
        registry = ActiveBundleRegistry()
    }

    override func tearDown() async throws {
        store?.removeAll()
        try await super.tearDown()
    }

    func test_hashOnHitCost_scalesWithBundleBytes() async throws {
        try requireProfiling()

        for megabytes in [1, 4, 16, 64] {
            let bytes = megabytes * 1024 * 1024
            let identity = AssetBundleIdentity(bundleID: "profile-\(megabytes)mb", version: 1)
            let body = Data(repeating: 0x41, count: bytes)
            let digest = SHA256.hash(data: body).map { String(format: "%02x", $0) }.joined()

            let manifest = Self.manifest(identity: identity, byteSize: Int64(bytes), digest: digest)

            try store.prepare(for: identity)
            try body.write(to: store.installedFileURL(identity, assetID: "geometry"))

            let cache = AssetBundleCache(store: store, policy: AssetCachePolicy(budgetBytes: 1 << 40), retention: registry)
            _ = try await cache.commit(manifest)

            _ = await cache.hit(manifest)
            let iterations = megabytes >= 16 ? 3 : 10
            let start = Date()
            for _ in 0..<iterations {
                guard await cache.hit(manifest) != nil else {
                    return XCTFail("a committed bundle must hit")
                }
            }
            let ms = Date().timeIntervalSince(start) * 1000 / Double(iterations)
            let throughput = Double(megabytes) / (ms / 1000)
            print(String(format: "MEASURED hash-on-hit: %3d MiB bundle → %7.2f ms per hit (%.0f MiB/s)", megabytes, ms, throughput))

            store.removeVersion(identity)
            _ = cache
        }
        print("""
        NOTE: hash-on-hit is paid ONCE per Room entry, since the two
        resolutions were collapsed into one. The number that matters for a
        real Room is throughput × the authored bundle's size, and no authored
        bundle exists — so this is the slope, not a verdict.
        """)
    }

    func test_evictionCost_atManyEntries() async throws {
        try requireProfiling()

        let cache = AssetBundleCache(store: store, policy: AssetCachePolicy(budgetBytes: 1 << 40), retention: registry)
        let body = Data(repeating: 0x42, count: 4096)
        let digest = SHA256.hash(data: body).map { String(format: "%02x", $0) }.joined()

        for count in [50, 200, 800] {
            store.removeAll()
            for index in 0..<count {
                let identity = AssetBundleIdentity(bundleID: "evict-\(index)", version: 1)
                let manifest = Self.manifest(identity: identity, byteSize: Int64(body.count), digest: digest)
                try store.prepare(for: identity)
                try body.write(to: store.installedFileURL(identity, assetID: "geometry"))
                _ = try await cache.commit(manifest)
            }
            let start = Date()
            let report = await cache.enforceBudget()
            let ms = Date().timeIntervalSince(start) * 1000
            print(String(format: "MEASURED eviction pass: %4d entries → %6.3f ms (evicted %d)", count, ms, report.evicted.count))
        }
    }

    private static func manifest(identity: AssetBundleIdentity, byteSize: Int64, digest: String) -> AssetBundleManifest {
        AssetBundleManifest(
            identity: identity,
            kind: .roomVariant,
            format: "usda",
            minAppVersion: 1,
            files: [AssetBundleFile(
                assetID: "geometry", role: .geometry,
                url: URL(string: "https://example.invalid/\(identity.bundleID)/geometry")!,
                contentType: "model/vnd.usda", byteSize: byteSize, checksumSHA256: digest
            )]
        )
    }

    func test_reconcileCost_atManyEntries() async throws {
        try requireProfiling()

        let body = Data(repeating: 0x43, count: 4096)
        let digest = SHA256.hash(data: body).map { String(format: "%02x", $0) }.joined()

        for count in [200, 800] {
            store.removeAll()
            let cache = AssetBundleCache(store: store, policy: AssetCachePolicy(budgetBytes: 1 << 40), retention: registry)
            for index in 0..<count {
                let identity = AssetBundleIdentity(bundleID: "recon-\(index)", version: 1)
                let manifest = Self.manifest(identity: identity, byteSize: Int64(body.count), digest: digest)
                try store.prepare(for: identity)
                try body.write(to: store.installedFileURL(identity, assetID: "geometry"))
                _ = try await cache.commit(manifest)
            }

            var start = Date()
            let budgetOnly = await cache.enforceBudget()
            let budgetMs = Date().timeIntervalSince(start) * 1000

            start = Date()
            let report = await cache.reconcile()
            let reconcileMs = Date().timeIntervalSince(start) * 1000

            XCTAssertEqual(report.validEntries, count, "reconcile must still see every valid entry")
            XCTAssertEqual(report.garbageRemoved, [], "no valid entry may be mistaken for debris")
            XCTAssertEqual(report.eviction.evicted, [], "nothing is over budget, so nothing may be evicted")
            XCTAssertEqual(budgetOnly.evicted.count, 0)
            let survivors = await cache.entries().count
            XCTAssertEqual(survivors, count, "the tree is intact after reconcile")
            print(String(format: "context (NOT a result — simulator variance exceeds the effect): %4d entries → enforceBudget %6.2f ms, reconcile() %6.2f ms",
                         count, budgetMs, reconcileMs))
        }
    }

    func test_leaseRegistryCost_underManyLeases() throws {
        try requireProfiling()

        var leases: [AssetBundleLease] = []
        for index in 0..<1000 {
            leases.append(registry.retain(AssetBundleIdentity(bundleID: "lease-\(index)", version: 1)))
        }
        let probe = AssetBundleIdentity(bundleID: "lease-999", version: 1)
        let ms = meanMilliseconds(iterations: 10_000) {
            _ = registry.isActive(probe)
        }
        print(String(format: "MEASURED lease lookup with 1000 held leases: %.6f ms per isActive()", ms))
        XCTAssertLessThan(ms, 0.01, "isActive must be a dictionary lookup, not a scan")

        for lease in leases { registry.release(lease) }
        XCTAssertFalse(registry.isActive(probe), "every lease released")
    }
}

// MARK: - Large payload decoding

final class DecodingProfileTests: XCTestCase {

    func test_largeCollectionRoomPayloadDecoding() throws {
        try requireProfiling()

        for itemCount in [250, 1000, 5000] {
            var items: [String] = []
            items.reserveCapacity(itemCount)
            for index in 0..<itemCount {
                items.append("""
                {"id":"\(UUID().uuidString)","slot_index":\(index),"catalog_model_id":"dev-fixture:model-chrono-one"}
                """)
            }
            let json = """
            {"id":"\(UUID().uuidString)","account_id":"\(UUID().uuidString)","name":"Profile Room",
             "category_id":"category_watches","design_id":"dev-fixture:collection-design",
             "current_tier":1,"music_track_id":null,
             "created_at":"2026-08-27T12:00:00Z","updated_at":"2026-08-27T12:00:00Z",
             "items":[\(items.joined(separator: ","))]}
            """
            let data = Data(json.utf8)

            let decoder = JSONDecoder()
            decoder.dateDecodingStrategy = .rfc3339
            let first = try decoder.decode(CollectionRoomResponseProbe.self, from: data)
            XCTAssertEqual(first.items.count, itemCount)

            let ms = try meanMilliseconds(iterations: 10) {
                _ = try decoder.decode(CollectionRoomResponseProbe.self, from: data)
            }
            print(String(format: "MEASURED decode: Collection Room with %4d items (%6.1f KiB) → %6.2f ms",
                         itemCount, Double(data.count) / 1024, ms))
        }
    }

    func test_fullMuseumRoomPayloadDecoding() throws {
        try requireProfiling()

        var slots: [String] = []
        for index in 0..<28 {
            slots.append("""
            {"slot_index":\(index),"photo_asset_id":"\(UUID().uuidString)","caption":"\(String(repeating: "caption text ", count: 20))"}
            """)
        }
        let json = """
        {"id":"\(UUID().uuidString)","name":"Full Room","variant_id":"style_modern_variant_Hall",
         "privacy":"public","music_track_id":null,
         "photo_slots":[\(slots.joined(separator: ","))],
         "sculptures":[{"slot_index":0,"catalog_id":"s1"},{"slot_index":1,"catalog_id":"s2"},{"slot_index":2,"catalog_id":"s3"}]}
        """
        let data = Data(json.utf8)
        let decoder = JSONDecoder()

        let room = try decoder.decode(MuseumRoomResponseProbe.self, from: data)
        XCTAssertEqual(room.photo_slots.count, 28)
        XCTAssertEqual(room.sculptures.count, 3)

        let ms = try meanMilliseconds(iterations: 100) {
            _ = try decoder.decode(MuseumRoomResponseProbe.self, from: data)
        }
        print(String(format: "MEASURED decode: full Museum Room (28 photos + 3 sculptures, %.1f KiB) → %.3f ms",
                     Double(data.count) / 1024, ms))
        XCTAssertLessThan(ms, 20, "a single Room payload decode must not approach a frame budget")
    }
}

private struct CollectionRoomResponseProbe: Decodable {
    struct Item: Decodable {
        let id: String
        let slot_index: Int
        let catalog_model_id: String
    }
    let id: String
    let name: String
    let current_tier: Int
    let items: [Item]
}

private struct MuseumRoomResponseProbe: Decodable {
    struct Slot: Decodable {
        let slot_index: Int
        let photo_asset_id: String?
        let caption: String
    }
    struct Sculpture: Decodable {
        let slot_index: Int
        let catalog_id: String
    }
    let id: String
    let name: String
    let photo_slots: [Slot]
    let sculptures: [Sculpture]
}

// MARK: - Slot resolution (documented handoff)

final class SlotResolutionProfileTests: XCTestCase {

    func test_singleSlotLookupVersusBulkUnion() throws {
        try requireProfiling()

        for tierCount in [25, 100, 400] {
            let slotsPerTier = 25
            let total = tierCount * slotsPerTier
            let table = Self.makeTable(tierCount: tierCount, slotsPerTier: slotsPerTier)
            guard let top = table.highestTier else { return XCTFail("no tiers") }

            let oneAtATime = meanMilliseconds(iterations: 1) {
                for slotIndex in 0..<total {
                    _ = table.slot(forSlotIndex: slotIndex, atTier: top)
                }
            }
            let union = meanMilliseconds(iterations: 1) {
                _ = table.availableSlots(atTier: top)
            }
            print(String(format: "MEASURED slot resolution: %3d tiers / %5d slots — one-at-a-time %8.2f ms, bulk union %6.3f ms (%.0f× )",
                         tierCount, total, oneAtATime, union, oneAtATime / max(union, 0.0001)))
        }
    }

    private static func makeTable(tierCount: Int, slotsPerTier: Int) -> CollectionTierTable {
        makeTierTable(capacities: (1...tierCount).map { $0 * slotsPerTier })
    }
}

// MARK: - Object lifetime under repeated navigation

@MainActor
final class LifetimeProfileTests: XCTestCase {

    private func makeController(photoCount: Int) -> RealityKitSceneViewController {
        let content = RoomRenderingVerificationFixture.makeContent(photoCount: photoCount)!
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        return controller
    }

    private func waitUntil(_ timeout: TimeInterval = 20, _ condition: @escaping @MainActor () -> Bool) async {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition() && Date() < deadline {
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
    }

    func test_roomScene_releasesEveryObjectItCreates() async {
        weak var controller: RealityKitSceneViewController?
        weak var photoLayer: RoomPhotoLayer?
        weak var captionLayer: RoomCaptionLayer?
        weak var sculptureLayer: RoomSculptureLayer?
        weak var camera: RealityKitCameraController?
        weak var coordinator: RoomContentEditCoordinator?

        do {
            let live = makeController(photoCount: 14)
            controller = live
            live.viewWillAppear(false)
            await waitUntil { live.texturedPhotoCount == 14 }

            photoLayer = live.photoLayer
            captionLayer = live.captionLayer
            sculptureLayer = live.sculptureLayer
            camera = live.cameraController
            coordinator = live.contentCoordinator

            XCTAssertNotNil(photoLayer, "precondition: the scene mounted a photo layer")
            XCTAssertNotNil(camera, "precondition: the scene mounted a camera rig")

            live.viewDidDisappear(false)
            XCTAssertNil(live.photoLayer, "the controller must drop its layers on teardown")
            XCTAssertFalse(live.isSceneLoaded)
        }

        await waitUntil(5) { controller == nil }

        XCTAssertNil(photoLayer, "RoomPhotoLayer outlived its scene")
        XCTAssertNil(captionLayer, "RoomCaptionLayer outlived its scene")
        XCTAssertNil(sculptureLayer, "RoomSculptureLayer outlived its scene")
        XCTAssertNil(camera, "RealityKitCameraController outlived its scene")
        XCTAssertNil(coordinator, "RoomContentEditCoordinator outlived its scene")
        XCTAssertNil(controller, "the scene view controller itself outlived its own teardown")
    }

    func test_repeatedNavigation_accumulatesNothing() async throws {
        try requireProfiling()

        var stillAlive: [Int] = []
        var weakControllers: [() -> Bool] = []

        for cycle in 0..<40 {
            weak var controller: RealityKitSceneViewController?
            do {
                let live = makeController(photoCount: 7)
                controller = live
                live.viewWillAppear(false)
                await waitUntil { live.texturedPhotoCount == 7 }
                live.viewDidDisappear(false)
            }
            weakControllers.append { controller != nil }
            if cycle % 10 == 9 {
                let observed = weakControllers
                await waitUntil(5) { observed.allSatisfy { !$0() } }
                stillAlive.append(observed.filter { $0() }.count)
            }
        }

        print("MEASURED repeated navigation: controllers still alive after 10/20/30/40 cycles: \(stillAlive)")
        XCTAssertEqual(stillAlive, [0, 0, 0, 0],
                       "a scene controller survived its own teardown — one leaked object per Room entry accumulates")
    }
}

// MARK: - Photo texture decoding

final class TextureProfileTests: XCTestCase {

    func test_photoTextureDecodeCost_atThePolicySizes() throws {
        try requireProfiling()

        let source = RoomRenderingVerificationFixture.FixturePhotoDownloader.renderJPEG(slot: 0, width: 3072, height: 2304)
        print(String(format: "source: 3072×2304 JPEG, %.0f KiB", Double(source.count) / 1024))

        for maxLongEdge in [1024, 768] {
            let decoded = try PhotoTextureDecoder.decode(source, maxLongEdge: maxLongEdge)
            XCTAssertLessThanOrEqual(max(decoded.pixelWidth, decoded.pixelHeight), maxLongEdge)

            let ms = try meanMilliseconds(iterations: 10) {
                _ = try PhotoTextureDecoder.decode(source, maxLongEdge: maxLongEdge)
            }
            let photos = maxLongEdge == 1024 ? 18 : 28
            print(String(format: "MEASURED texture decode: → %4d px long edge: %5.2f ms each; %d photos = %6.1f ms of decode",
                         maxLongEdge, ms, photos, ms * Double(photos)))
        }
        print("""
        NOTE: decode only. Upload to a RealityKit `TextureResource`, GPU
        residency and draw cost are NOT measured — that needs the final
        scene and authored materials, which do not exist yet. Decode runs
        off the main actor, so this is throughput, not a frame-time budget.
        """)
    }
}
