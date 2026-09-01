import UIKit
import XCTest
@testable import MuseApp

@MainActor
final class DeliveredBundleLeaseTests: XCTestCase {

    private func fixtureFile(_ name: String) -> URL? {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
        let url = root
            .appendingPathComponent("assets/dev_fixtures/bundles/dev_fixture_room_variant/v1")
            .appendingPathComponent(name)
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
    }

    private func deliveredContent(retention: ActiveBundleRegistry) throws -> (RoomRuntimeContent, AssetBundleIdentity) {
        guard let geometryURL = fixtureFile("geometry.usda"), let layoutURL = fixtureFile("layout.json") else {
            throw XCTSkip("assets/dev_fixtures is not reachable from this test host")
        }
        let layout = try RoomVariantLayoutFile.decode(contentsOf: layoutURL)
        let identity = AssetBundleIdentity(bundleID: "dev_fixture_room_variant", version: 1)
        let geometry = RoomVariantGeometry(
            variantID: layout.table.variantID, identity: identity, format: "usda",
            fileURL: geometryURL, entry: layout.entry
        )
        let content = try XCTUnwrap(RoomRenderingVerificationFixture.makeContent(
            photoCount: 2, deliveredGeometry: geometry, deliveredSlotTable: layout.table, bundleRetention: retention
        ))
        return (content, identity)
    }

    func test_aMountedDeliveredBundle_isActiveForTheLifeOfTheScene() throws {
        let registry = ActiveBundleRegistry()
        let (content, identity) = try deliveredContent(retention: registry)
        XCTAssertFalse(registry.isActive(identity), "nothing is active before a scene exists")

        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertTrue(registry.isActive(identity), "mounting the geometry must take the lease")
        XCTAssertEqual(controller.activeBundleLease?.identity, identity)

        controller.viewDidDisappear(false)

        XCTAssertFalse(registry.isActive(identity), "tearing the scene down must release it")
        XCTAssertNil(controller.activeBundleLease)
    }

    func test_reenteringTheScene_takesAFreshLease() throws {
        let registry = ActiveBundleRegistry()
        let (content, identity) = try deliveredContent(retention: registry)
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()

        for _ in 0..<3 {
            controller.viewWillAppear(false)
            XCTAssertEqual(registry.leaseCount(for: identity), 1, "exactly one lease per live scene, never accumulating")
            controller.viewDidDisappear(false)
            XCTAssertEqual(registry.leaseCount(for: identity), 0)
        }
    }

    func test_twoScenesOnOneBundle_keepItActiveUntilBothLeave() throws {
        let registry = ActiveBundleRegistry()
        let (content, identity) = try deliveredContent(retention: registry)
        let first = RealityKitSceneViewController(content: content)
        let second = RealityKitSceneViewController(content: content)
        first.loadViewIfNeeded(); second.loadViewIfNeeded()

        first.viewWillAppear(false)
        second.viewWillAppear(false)
        XCTAssertEqual(registry.leaseCount(for: identity), 2)

        first.viewDidDisappear(false)
        XCTAssertTrue(registry.isActive(identity), "the second scene still displays it")
        second.viewDidDisappear(false)
        XCTAssertFalse(registry.isActive(identity))
    }

    func test_theFixtureAndTheSkeleton_takeNoLease() {
        let registry = ActiveBundleRegistry()
        let fixture = RealityKitSceneViewController(
            content: RoomRenderingVerificationFixture.makeContent(photoCount: 1, bundleRetention: registry)
        )
        fixture.loadViewIfNeeded()
        fixture.viewWillAppear(false)
        XCTAssertNil(fixture.activeBundleLease)
        XCTAssertTrue(registry.activeIdentities.isEmpty)
        fixture.viewDidDisappear(false)

        let skeleton = RealityKitSceneViewController(content: nil)
        skeleton.loadViewIfNeeded()
        skeleton.viewWillAppear(false)
        XCTAssertNil(skeleton.activeBundleLease)
        skeleton.viewDidDisappear(false)
    }

    func test_aDisplayedBundle_survivesEvictionPressure() async throws {
        let registry = ActiveBundleRegistry()
        let (content, identity) = try deliveredContent(retention: registry)
        let root = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("MuseLease-\(UUID().uuidString)")
        let store = AssetBundleStore(root: root)
        defer { store.removeAll() }

        let cache = AssetBundleCache(store: store, policy: AssetCachePolicy(budgetBytes: 1), retention: registry)
        let manifest = try installFixtureFiles(identity: identity, into: store)
        _ = try await cache.commit(manifest)

        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        let underPressure = await cache.enforceBudget()
        XCTAssertTrue(underPressure.evicted.isEmpty, "the scene holds a lease; the pass must not touch it")
        XCTAssertGreaterThan(underPressure.remainingOverBudget, 0)
        let stillCached = await cache.entries().map(\.identity)
        XCTAssertEqual(stillCached, [identity])

        controller.viewDidDisappear(false)
        let afterLeaving = await cache.enforceBudget()
        XCTAssertEqual(afterLeaving.evicted, [identity], "once the scene is gone, the same pass evicts it")
    }

    func test_roomEntry_throughRealDelivery_leasesForTheSceneLifetime_andHitsTheCacheNextTime() async throws {
        guard fixtureFile("geometry.usda") != nil else {
            throw XCTSkip("assets/dev_fixtures is not reachable from this test host")
        }
        URLProtocol.registerClass(RangeAwareOriginProtocol.self)
        defer { RangeAwareOriginProtocol.reset(); URLProtocol.unregisterClass(RangeAwareOriginProtocol.self) }
        RangeAwareOriginProtocol.reset()

        let root = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("MuseEntryLease-\(UUID().uuidString)")
        let store = AssetBundleStore(root: root)
        defer { store.removeAll() }
        let registry = ActiveBundleRegistry()
        let source = StubManifestSource()
        let bundles = AssetBundleDeliveryService(
            manifests: source,
            downloader: URLSessionAssetBundleDownloader(session: RangeAwareOriginProtocol.session()),
            cache: AssetBundleCache(store: store, retention: registry)
        )
        let variants = StubVariantLookup(variants: [
            "dev-fixture:room-variant": RoomVariant(id: "dev-fixture:room-variant", styleID: "n/a", displayName: "Fixture",
                                                    assetBundle: AssetBundleRef(id: "dev_fixture_room_variant", version: 1))
        ])
        let design = DeliveredVariantLayoutProvider(bundles: bundles, variants: variants, accessToken: { "t" })
        let identity = AssetBundleIdentity(bundleID: "dev_fixture_room_variant", version: 1)
        BundleFixture.publish(bundleID: "dev_fixture_room_variant", version: 1, into: source)
        _ = registry

        let room = Room(id: "r", name: "Study", variantID: "dev-fixture:room-variant", privacy: .private,
                        photoSlots: [PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a0", caption: "")])

        let first = RoomEntryViewModel(room: room, design: design, textures: NoTextures(), accessToken: "t", bundleRetention: registry)
        await first.load()
        XCTAssertEqual(first.state, .ready)
        let downloads = RangeAwareOriginProtocol.log.filter { $0.url.hasSuffix("/geometry") }.count
        XCTAssertEqual(downloads, 1)
        XCTAssertFalse(registry.isActive(identity), "resolving a design takes no lease — only displaying it does")

        let controller = RealityKitSceneViewController(content: try XCTUnwrap(first.content))
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        XCTAssertTrue(registry.isActive(identity), "the mounted scene holds the lease")
        controller.viewDidDisappear(false)
        XCTAssertFalse(registry.isActive(identity), "and releases it when torn down")

        let second = RoomEntryViewModel(room: room, design: design, textures: NoTextures(), accessToken: "t", bundleRetention: registry)
        let recorder = StateSequenceRecorder()
        second.onStateChange = { recorder.record($0) }
        await second.load()
        XCTAssertEqual(second.state, .ready)
        XCTAssertEqual(RangeAwareOriginProtocol.log.filter { $0.url.hasSuffix("/geometry") }.count, downloads, "a cache hit fetches nothing")
        XCTAssertFalse(recorder.states.contains { if case .downloading = $0 { return true } else { return false } },
                       "a cache hit shows no download state — near-instant, as 02 requires")
    }

    private func installFixtureFiles(identity: AssetBundleIdentity, into store: AssetBundleStore) throws -> AssetBundleManifest {
        let geometry = try Data(contentsOf: try XCTUnwrap(fixtureFile("geometry.usda")))
        let layout = try Data(contentsOf: try XCTUnwrap(fixtureFile("layout.json")))
        try store.prepare(for: identity)
        try geometry.write(to: store.installedFileURL(identity, assetID: "geometry"))
        try layout.write(to: store.installedFileURL(identity, assetID: "layout"))
        return AssetBundleManifest(
            identity: identity, kind: .roomVariant, format: "usda", minAppVersion: 1,
            files: [
                AssetBundleFile(assetID: "geometry", role: .geometry, url: URL(string: "https://assets.example/g")!,
                                contentType: "model/vnd.usda+ascii", byteSize: Int64(geometry.count),
                                checksumSHA256: BundleFixture.sha256Hex(geometry)),
                AssetBundleFile(assetID: "layout", role: .layout, url: URL(string: "https://assets.example/l")!,
                                contentType: "application/json", byteSize: Int64(layout.count),
                                checksumSHA256: BundleFixture.sha256Hex(layout))
            ]
        )
    }
}
