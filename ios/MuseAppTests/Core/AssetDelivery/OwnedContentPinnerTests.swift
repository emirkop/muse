import XCTest
@testable import MuseApp

final class OwnedContentPinnerTests: XCTestCase {

    private func museum(style: String = "style_modern") -> Museum {
        Museum(id: "m1", styleID: style, privacy: .private)
    }

    private func room(_ id: String, variant: String) -> Room {
        Room(id: id, name: id, variantID: variant, privacy: .private)
    }

    private func makePinner(service: FakeMuseumService, registry: ActiveBundleRegistry) -> OwnedContentPinner {
        OwnedContentPinner(museums: service, catalog: service, retention: registry)
    }

    private let styleBundle = AssetBundleIdentity(bundleID: "b1", version: 1)
    private let hallBundle = AssetBundleIdentity(bundleID: "bv1", version: 1)
    private let galleryBundle = AssetBundleIdentity(bundleID: "bv2", version: 1)

    func test_pinsTheOwnersStyleAndEveryRoomsVariant() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(museum())
        service.roomsResult = .success([room("r1", variant: "v1"), room("r2", variant: "v2"), room("r3", variant: "v1")])
        let registry = ActiveBundleRegistry()
        let pinner = makePinner(service: service, registry: registry)

        let refresh = await pinner.refresh(accessToken: "t")

        XCTAssertEqual(refresh.pinned, [styleBundle, hallBundle, galleryBundle])
        XCTAssertEqual(refresh.newlyPinned, refresh.pinned)
        XCTAssertTrue(registry.isActive(styleBundle))
        XCTAssertTrue(registry.isActive(hallBundle))
        XCTAssertTrue(registry.isActive(galleryBundle))
        XCTAssertEqual(registry.leaseCount(for: hallBundle), 1)
        XCTAssertFalse(refresh.unchangedBecauseUnreadable)
    }

    func test_refresh_isIdempotent() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(museum())
        service.roomsResult = .success([room("r1", variant: "v1")])
        let registry = ActiveBundleRegistry()
        let pinner = makePinner(service: service, registry: registry)

        _ = await pinner.refresh(accessToken: "t")
        let second = await pinner.refresh(accessToken: "t")

        XCTAssertTrue(second.newlyPinned.isEmpty)
        XCTAssertTrue(second.released.isEmpty)
        XCTAssertEqual(registry.leaseCount(for: styleBundle), 1, "a refresh never stacks leases")
    }

    func test_contentChanges_releaseWhatIsNoLongerReferenced() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(museum())
        service.roomsResult = .success([room("r1", variant: "v1"), room("r2", variant: "v2")])
        let registry = ActiveBundleRegistry()
        let pinner = makePinner(service: service, registry: registry)
        _ = await pinner.refresh(accessToken: "t")

        service.roomsResult = .success([room("r1", variant: "v1"), room("r2", variant: "v1")])
        let refresh = await pinner.refresh(accessToken: "t")

        XCTAssertEqual(refresh.released, [galleryBundle])
        XCTAssertFalse(registry.isActive(galleryBundle), "an unreferenced bundle is eligible for eviction again")
        XCTAssertTrue(registry.isActive(hallBundle))
        XCTAssertTrue(registry.isActive(styleBundle))
    }

    func test_noMuseum_pinsNothing_andReleasesAnyPreviousPins() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(museum())
        service.roomsResult = .success([room("r1", variant: "v1")])
        let registry = ActiveBundleRegistry()
        let pinner = makePinner(service: service, registry: registry)
        _ = await pinner.refresh(accessToken: "t")
        XCTAssertTrue(registry.isActive(styleBundle))

        service.fetchResult = .failure(IdentityAPIClientError.server(statusCode: 404, message: "not found"))
        let refresh = await pinner.refresh(accessToken: "t")

        XCTAssertTrue(refresh.pinned.isEmpty)
        XCTAssertEqual(refresh.released, [styleBundle, hallBundle])
        XCTAssertTrue(registry.activeIdentities.isEmpty)
    }

    func test_transientFailure_keepsExistingPins() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(museum())
        service.roomsResult = .success([room("r1", variant: "v1")])
        let registry = ActiveBundleRegistry()
        let pinner = makePinner(service: service, registry: registry)
        _ = await pinner.refresh(accessToken: "t")

        service.roomsResult = .failure(IdentityAPIClientError.transport)
        let refresh = await pinner.refresh(accessToken: "t")

        XCTAssertTrue(refresh.unchangedBecauseUnreadable)
        XCTAssertEqual(refresh.pinned, [styleBundle, hallBundle])
        XCTAssertTrue(registry.isActive(styleBundle))
        XCTAssertTrue(registry.isActive(hallBundle))
    }

    func test_clear_releasesEverything() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(museum())
        service.roomsResult = .success([room("r1", variant: "v1")])
        let registry = ActiveBundleRegistry()
        let pinner = makePinner(service: service, registry: registry)
        _ = await pinner.refresh(accessToken: "t")

        await pinner.clear()

        XCTAssertTrue(registry.activeIdentities.isEmpty)
        let pinned = await pinner.pinnedIdentities
        XCTAssertTrue(pinned.isEmpty)
    }

    func test_aPinAndASceneLease_onOneBundle_areIndependent() async {
        let service = FakeMuseumService()
        service.fetchResult = .success(museum())
        service.roomsResult = .success([room("r1", variant: "v1")])
        let registry = ActiveBundleRegistry()
        let pinner = makePinner(service: service, registry: registry)
        _ = await pinner.refresh(accessToken: "t")

        let sceneLease = registry.retain(hallBundle)
        XCTAssertEqual(registry.leaseCount(for: hallBundle), 2)

        registry.release(sceneLease)
        XCTAssertTrue(registry.isActive(hallBundle), "the owner's pin still protects their own Room's Variant")

        service.roomsResult = .success([])
        _ = await pinner.refresh(accessToken: "t")
        XCTAssertFalse(registry.isActive(hallBundle))
    }

    // MARK: The eviction consequence, end to end

    func test_pinnedOwnerBundle_survivesEvictionPressure_withNoSceneMounted() async throws {
        let root = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("MuseOwnerPin-\(UUID().uuidString)")
        let store = AssetBundleStore(root: root)
        defer { store.removeAll() }
        let registry = ActiveBundleRegistry()
        let cache = AssetBundleCache(store: store, policy: AssetCachePolicy(budgetBytes: 1), retention: registry)

        let identity = AssetBundleIdentity(bundleID: "bv1", version: 1)
        let manifest = try installTinyBundle(identity: identity, into: store)
        _ = try await cache.commit(manifest)

        let service = FakeMuseumService()
        service.fetchResult = .success(museum())
        service.roomsResult = .success([room("r1", variant: "v1")])
        _ = await OwnedContentPinner(museums: service, catalog: service, retention: registry).refresh(accessToken: "t")

        let report = await cache.enforceBudget()
        XCTAssertTrue(report.evicted.isEmpty, "the owner's own Variant is pinned; a budget pass must not evict it")
        XCTAssertGreaterThan(report.remainingOverBudget, 0)
    }

    private func installTinyBundle(identity: AssetBundleIdentity, into store: AssetBundleStore) throws -> AssetBundleManifest {
        let geometry = Data("#usda 1.0\n".utf8)
        try store.prepare(for: identity)
        try geometry.write(to: store.installedFileURL(identity, assetID: "geometry"))
        return AssetBundleManifest(
            identity: identity, kind: .roomVariant, format: "usda", minAppVersion: 1,
            files: [AssetBundleFile(assetID: "geometry", role: .geometry, url: URL(string: "https://assets.example/g")!,
                                    contentType: "model/vnd.usda+ascii", byteSize: Int64(geometry.count),
                                    checksumSHA256: BundleFixture.sha256Hex(geometry))]
        )
    }
}
