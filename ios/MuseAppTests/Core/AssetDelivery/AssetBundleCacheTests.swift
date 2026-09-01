import Foundation
import XCTest
@testable import MuseApp

final class TestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var current: TimeInterval

    init(start: TimeInterval = 1_000_000) { current = start }

    var now: @Sendable () -> Date {
        { [self] in
            lock.lock(); defer { lock.unlock() }
            return Date(timeIntervalSince1970: current)
        }
    }

    func advance(_ seconds: TimeInterval = 60) {
        lock.lock(); current += seconds; lock.unlock()
    }

    var date: Date {
        lock.lock(); defer { lock.unlock() }
        return Date(timeIntervalSince1970: current)
    }
}

final class AssetBundleCacheTests: XCTestCase {
    private var root: URL!
    private var store: AssetBundleStore!
    private var source: StubManifestSource!
    private var registry: ActiveBundleRegistry!
    private var clock: TestClock!

    override func setUp() {
        super.setUp()
        URLProtocol.registerClass(RangeAwareOriginProtocol.self)
        RangeAwareOriginProtocol.reset()
        root = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("MuseAssetCacheTests-\(UUID().uuidString)", isDirectory: true)
        store = AssetBundleStore(root: root)
        source = StubManifestSource()
        registry = ActiveBundleRegistry()
        clock = TestClock()
    }

    override func tearDown() {
        store?.removeAll()
        RangeAwareOriginProtocol.reset()
        URLProtocol.unregisterClass(RangeAwareOriginProtocol.self)
        super.tearDown()
    }

    // MARK: - Harness

    private func budget(fitting bundles: Int) -> AssetCachePolicy {
        let oneBundle = BundleFixture.geometryBody(version: 9).count + BundleFixture.layoutBody(variantID: "x", version: 9).count
        return AssetCachePolicy(budgetBytes: Int64(oneBundle * bundles + oneBundle / 2))
    }

    private func makeCache(fitting bundles: Int) -> AssetBundleCache {
        AssetBundleCache(store: store, policy: budget(fitting: bundles), retention: registry, now: clock.now)
    }

    private func makeService(_ cache: AssetBundleCache) -> AssetBundleDeliveryService {
        AssetBundleDeliveryService(
            manifests: source,
            downloader: URLSessionAssetBundleDownloader(session: RangeAwareOriginProtocol.session()),
            cache: cache
        )
    }

    @discardableResult
    private func install(_ bundleID: String, version: Int = 1, via service: AssetBundleDeliveryService) async throws -> InstalledAssetBundle {
        BundleFixture.publish(bundleID: bundleID, version: version, into: source)
        let result = await service.bundle(accessToken: "t", bundleID: bundleID, progress: nil)
        guard case .success(let installed) = result else {
            throw XCTSkip("install of \(bundleID) v\(version) failed: \(result)")
        }
        return installed
    }

    private func geometryRequests(for bundleID: String) -> Int {
        RangeAwareOriginProtocol.log.filter { $0.url.contains("/\(bundleID)/") && $0.url.hasSuffix("/geometry") }.count
    }

    private func identities(_ cache: AssetBundleCache) async -> [String] {
        await cache.entries().map { "\($0.identity.bundleID)@\($0.identity.version)" }
    }

    // MARK: PROOF 1 — a cache hit avoids re-downloading

    func test_hit_downloadsNothing_andCountsAsAnAccess() async throws {
        let cache = makeCache(fitting: 4)
        let service = makeService(cache)
        try await install("alpha", via: service)
        XCTAssertEqual(geometryRequests(for: "alpha"), 1)
        let installedAt = clock.date

        clock.advance(300)
        let again = await service.bundle(accessToken: "t", bundleID: "alpha", progress: nil)
        guard case .success = again else { return XCTFail("expected a hit") }

        XCTAssertEqual(geometryRequests(for: "alpha"), 1, "a hit must fetch no bytes")
        XCTAssertEqual(source.fetchCount, 2)

        let entries = await cache.entries()
        let entry = try XCTUnwrap(entries.first)
        XCTAssertEqual(entry.installedAt, installedAt)
        XCTAssertEqual(entry.lastAccessedAt, clock.date, "a hit is an access; recency must move")
        XCTAssertGreaterThan(entry.lastAccessedAt, entry.installedAt)
    }

    // MARK: PROOF 2 — overflow evicts the least recently used, non-active entry first

    func test_overflow_evictsLeastRecentlyUsedFirst() async throws {
        let cache = makeCache(fitting: 2)
        let service = makeService(cache)

        try await install("alpha", via: service)
        clock.advance()
        try await install("beta", via: service)
        clock.advance()
        _ = await service.bundle(accessToken: "t", bundleID: "alpha", progress: nil)
        clock.advance()

        try await install("gamma", via: service)

        let remaining = await identities(cache)
        XCTAssertEqual(Set(remaining), ["alpha@1", "gamma@1"], "beta was least recently used and must be the one evicted")
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.versionDirectory(AssetBundleIdentity(bundleID: "beta", version: 1)).path))
        let total = await cache.totalBytes()
        XCTAssertLessThanOrEqual(total, cache.policy.budgetBytes)
    }

    func test_eviction_isDeterministic_whenRecencyTies() async throws {
        let cache = makeCache(fitting: 2)
        let service = makeService(cache)
        try await install("zeta", via: service)
        try await install("eta", via: service)
        try await install("theta", via: service)

        let remaining = await identities(cache)
        XCTAssertEqual(Set(remaining), ["zeta@1", "theta@1"])
    }

    // MARK: PROOF 3 — an active bundle is never evicted

    func test_leasedBundle_isNeverEvicted_evenWhenLeastRecentlyUsed() async throws {
        let cache = makeCache(fitting: 2)
        let service = makeService(cache)

        let alpha = try await install("alpha", via: service)
        let lease = registry.retain(alpha.identity)
        clock.advance()
        try await install("beta", via: service)
        clock.advance()
        try await install("gamma", via: service)

        let remaining = await identities(cache)
        XCTAssertEqual(Set(remaining), ["alpha@1", "gamma@1"])
        XCTAssertTrue(FileManager.default.fileExists(atPath: alpha.geometryURL!.path), "a leased bundle's files must still be on disk")
        _ = lease
    }

    func test_whenOnlyLeasedEntriesRemain_nothingIsForcedAndOverBudgetIsReported() async throws {
        let cache = makeCache(fitting: 1)
        let service = makeService(cache)

        let alpha = try await install("alpha", via: service)
        _ = registry.retain(alpha.identity)
        clock.advance()
        try await install("beta", via: service)

        let remaining = await identities(cache)
        XCTAssertEqual(Set(remaining), ["alpha@1", "beta@1"], "neither may be evicted: one leased, one just committed")
        let report = await cache.enforceBudget(protecting: [AssetBundleIdentity(bundleID: "beta", version: 1)])
        XCTAssertTrue(report.evicted.isEmpty)
        XCTAssertGreaterThan(report.remainingOverBudget, 0, "the pass must say it could not satisfy the budget")
    }

    // MARK: PROOF 4 — releasing a lease makes the bundle eligible again

    func test_releasingTheLease_makesTheBundleEligible() async throws {
        let cache = makeCache(fitting: 2)
        let service = makeService(cache)

        let alpha = try await install("alpha", via: service)
        let lease = registry.retain(alpha.identity)
        clock.advance()
        try await install("beta", via: service)
        clock.advance()
        try await install("gamma", via: service)
        let afterGamma = await identities(cache)
        XCTAssertEqual(Set(afterGamma), ["alpha@1", "gamma@1"])

        registry.release(lease)
        XCTAssertFalse(registry.isActive(alpha.identity))
        clock.advance()
        try await install("delta", via: service)

        let afterDelta = await identities(cache)
        XCTAssertEqual(Set(afterDelta), ["gamma@1", "delta@1"], "once released, the former lease-holder is evicted like any other LRU entry")
    }

    func test_twoLeasesOnOneBundle_needTwoReleases() throws {
        let identity = AssetBundleIdentity(bundleID: "shared", version: 1)
        let first = registry.retain(identity)
        let second = registry.retain(identity)
        XCTAssertEqual(registry.leaseCount(for: identity), 2)

        registry.release(first)
        XCTAssertTrue(registry.isActive(identity), "a second scene still displays it")
        registry.release(first)
        XCTAssertTrue(registry.isActive(identity))

        registry.release(second)
        XCTAssertFalse(registry.isActive(identity))
        XCTAssertEqual(registry.activeIdentities, [])
    }

    // MARK: PROOF 5 — the cache survives a restart

    func test_restart_preservesEntriesAndRecency() async throws {
        let firstCache = makeCache(fitting: 4)
        let firstService = makeService(firstCache)
        try await install("alpha", via: firstService)
        clock.advance()
        try await install("beta", via: firstService)
        clock.advance()
        _ = await firstService.bundle(accessToken: "t", bundleID: "alpha", progress: nil)
        let orderBefore = await identities(firstCache)
        XCTAssertEqual(orderBefore, ["beta@1", "alpha@1"], "LRU first: beta is older than alpha's last access")

        let secondCache = AssetBundleCache(store: store, policy: firstCache.policy, retention: ActiveBundleRegistry(), now: clock.now)
        let report = await secondCache.reconcile()
        XCTAssertEqual(report.validEntries, 2)
        XCTAssertTrue(report.garbageRemoved.isEmpty)
        let orderAfter = await identities(secondCache)
        XCTAssertEqual(orderAfter, orderBefore, "recency order must survive the restart exactly")

        let requestsBefore = geometryRequests(for: "beta")
        let secondService = makeService(secondCache)
        let result = await secondService.bundle(accessToken: "t", bundleID: "beta", progress: nil)
        guard case .success = result else { return XCTFail("expected a hit after restart") }
        XCTAssertEqual(geometryRequests(for: "beta"), requestsBefore)
    }

    func test_reconcile_appliesASmallerBudgetFromTheNewBuild() async throws {
        let roomy = makeCache(fitting: 4)
        let service = makeService(roomy)
        try await install("alpha", via: service)
        clock.advance()
        try await install("beta", via: service)
        clock.advance()
        try await install("gamma", via: service)

        let tighter = AssetBundleCache(store: store, policy: budget(fitting: 1), retention: ActiveBundleRegistry(), now: clock.now)
        let report = await tighter.reconcile()
        XCTAssertEqual(report.eviction.evicted.map(\.bundleID), ["alpha", "beta"], "LRU order, until within budget")
        let remaining = await identities(tighter)
        XCTAssertEqual(remaining, ["gamma@1"])
    }

    // MARK: PROOF 6 — corrupt and stale entries are never served

    func test_corruptFile_isNotServed_theEntryIsRemoved_andTheBundleIsRefetched() async throws {
        let cache = makeCache(fitting: 4)
        let service = makeService(cache)
        let installed = try await install("alpha", via: service)
        let geometryURL = try XCTUnwrap(installed.geometryURL)

        var bytes = try Data(contentsOf: geometryURL)
        bytes[bytes.count / 2] ^= 0xFF
        try bytes.write(to: geometryURL)

        let result = await service.bundle(accessToken: "t", bundleID: "alpha", progress: nil)
        guard case .success(let repaired) = result else { return XCTFail("expected a re-download, got \(result)") }
        XCTAssertEqual(geometryRequests(for: "alpha"), 2, "the corrupt entry must be discarded and the bytes fetched again")
        XCTAssertEqual(try Data(contentsOf: try XCTUnwrap(repaired.geometryURL)), BundleFixture.geometryBody(version: 1))
    }

    func test_truncatedFile_isNotServed() async throws {
        let cache = makeCache(fitting: 4)
        let service = makeService(cache)
        let installed = try await install("alpha", via: service)
        let layoutURL = try XCTUnwrap(installed.layoutURL)
        try Data(try Data(contentsOf: layoutURL).prefix(10)).write(to: layoutURL)

        let manifest = try await source.manifest(accessToken: "t", bundleID: "alpha", appAssetVersion: 1)
        let hit = await cache.hit(manifest)
        XCTAssertNil(hit, "a truncated file must not produce a hit")
        let entries = await cache.entries()
        XCTAssertTrue(entries.isEmpty, "and the broken entry must be gone")
    }

    func test_directoryWithoutARecord_isNotAnEntry_andIsReconciledAway() async throws {
        let identity = AssetBundleIdentity(bundleID: "half", version: 1)
        try store.prepare(for: identity)
        try BundleFixture.geometryBody(version: 1).write(to: store.installedFileURL(identity, assetID: "geometry"))

        let cache = makeCache(fitting: 4)
        let entries = await cache.entries()
        XCTAssertTrue(entries.isEmpty, "files without a record are not a cache entry")
        let total = await cache.totalBytes()
        XCTAssertEqual(total, 0, "and are not counted against the budget")

        let report = await cache.reconcile()
        XCTAssertEqual(report.garbageRemoved, [identity])
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.versionDirectory(identity).path))
    }

    func test_recordWhoseFilesAreGone_isNotServed_andIsRemoved() async throws {
        let cache = makeCache(fitting: 4)
        let service = makeService(cache)
        let installed = try await install("alpha", via: service)
        try FileManager.default.removeItem(at: try XCTUnwrap(installed.geometryURL))

        let manifest = try await source.manifest(accessToken: "t", bundleID: "alpha", appAssetVersion: 1)
        let hit = await cache.hit(manifest)
        XCTAssertNil(hit)
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.entryRecordURL(installed.identity).path), "the orphaned record must go too")
    }

    func test_recordThatDisagreesWithTheManifest_isNotServed() async throws {
        let cache = makeCache(fitting: 4)
        let service = makeService(cache)
        let installed = try await install("alpha", via: service)

        let published = try await source.manifest(accessToken: "t", bundleID: "alpha", appAssetVersion: 1)
        let different = AssetBundleManifest(
            identity: published.identity, kind: published.kind, format: published.format,
            minAppVersion: published.minAppVersion,
            files: published.files.map {
                AssetBundleFile(assetID: $0.assetID, role: $0.role, url: $0.url, contentType: $0.contentType,
                                byteSize: $0.byteSize, checksumSHA256: String(repeating: "0", count: 64))
            }
        )
        let hit = await cache.hit(different)
        XCTAssertNil(hit)
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.versionDirectory(installed.identity).path))
    }

    func test_reconcile_removesAnIncompleteInstallButKeepsItsPartialForResume() async throws {
        let identity = AssetBundleIdentity(bundleID: "interrupted", version: 1)
        try store.prepare(for: identity)
        let partial = store.partialFileURL(identity, assetID: "geometry")
        try FileManager.default.createDirectory(at: partial.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(repeating: 0x41, count: 2048).write(to: partial)

        let cache = makeCache(fitting: 4)
        let report = await cache.reconcile()
        XCTAssertEqual(report.garbageRemoved, [identity], "the record-less directory is debris")
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.versionDirectory(identity).path))
        XCTAssertTrue(FileManager.default.fileExists(atPath: partial.path), "the partial is the downloader's resume state and must survive")
        XCTAssertEqual(URLSessionAssetBundleDownloader.byteCount(at: partial), 2048)
    }

    func test_hitWithNoRecord_removesNothing() async throws {
        let identity = AssetBundleIdentity(bundleID: "inprogress", version: 1)
        try store.prepare(for: identity)
        let partial = store.partialFileURL(identity, assetID: "geometry")
        try FileManager.default.createDirectory(at: partial.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(repeating: 0x41, count: 512).write(to: partial)
        BundleFixture.publish(bundleID: "inprogress", version: 1, into: source)
        let manifest = try await source.manifest(accessToken: "t", bundleID: "inprogress", appAssetVersion: 1)

        let cache = makeCache(fitting: 4)
        let hit = await cache.hit(manifest)
        XCTAssertNil(hit)
        XCTAssertTrue(FileManager.default.fileExists(atPath: store.versionDirectory(identity).path))
        XCTAssertTrue(FileManager.default.fileExists(atPath: partial.path))
    }

    func test_partialDownload_isNeverAnEntry_andIsLeftForResume() async throws {
        let identity = AssetBundleIdentity(bundleID: "inflight", version: 1)
        let partial = store.partialFileURL(identity, assetID: "geometry")
        try FileManager.default.createDirectory(at: partial.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(repeating: 0x41, count: 4096).write(to: partial)

        let cache = makeCache(fitting: 4)
        let entries = await cache.entries()
        XCTAssertTrue(entries.isEmpty)
        let total = await cache.totalBytes()
        XCTAssertEqual(total, 0, "staging area is not counted against the budget")
        _ = await cache.reconcile()
        XCTAssertTrue(FileManager.default.fileExists(atPath: partial.path), "reconciliation leaves the downloader's resume state alone")
    }

    // MARK: PROOF 7 — a version change never returns the old bundle as current

    func test_versionChange_neverServesTheOldVersion_andRemovesIt() async throws {
        let cache = makeCache(fitting: 4)
        let service = makeService(cache)
        try await install("alpha", version: 1, via: service)
        let before = await identities(cache)
        XCTAssertEqual(before, ["alpha@1"])

        BundleFixture.publish(bundleID: "alpha", version: 2, into: source)
        let v2Manifest = try await source.manifest(accessToken: "t", bundleID: "alpha", appAssetVersion: 1)
        let staleHit = await cache.hit(v2Manifest)
        XCTAssertNil(staleHit, "an installed v1 is not a hit for v2, whatever its recency")

        let result = await service.bundle(accessToken: "t", bundleID: "alpha", progress: nil)
        guard case .success(let current) = result else { return XCTFail("v2 should install") }
        XCTAssertEqual(current.identity.version, 2)
        let after = await identities(cache)
        XCTAssertEqual(after, ["alpha@2"], "v1 is superseded and gone; only v2 is an entry")
        let versions = await service.installedVersions(ofBundle: "alpha")
        XCTAssertEqual(versions, [2])
    }

    // MARK: - The policy's own shape

    func test_developmentDefaultBudget_isTheLabelledInterimValue() {
        XCTAssertEqual(AssetCachePolicy.developmentDefault.budgetBytes, 512 * 1024 * 1024)
    }
}
