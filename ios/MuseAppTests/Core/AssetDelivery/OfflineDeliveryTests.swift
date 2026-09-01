import XCTest
@testable import MuseApp

final class OfflineDeliveryTests: XCTestCase {
    private var root: URL!
    private var store: AssetBundleStore!
    private var source: StubManifestSource!
    private var registry: ActiveBundleRegistry!

    override func setUp() {
        super.setUp()
        URLProtocol.registerClass(RangeAwareOriginProtocol.self)
        RangeAwareOriginProtocol.reset()
        root = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("Muse-\(UUID().uuidString)", isDirectory: true)
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

    private func makeCache() -> AssetBundleCache {
        AssetBundleCache(store: store, policy: .developmentDefault, retention: registry)
    }

    private func makeService(cache: AssetBundleCache) -> AssetBundleDeliveryService {
        AssetBundleDeliveryService(
            manifests: source,
            downloader: URLSessionAssetBundleDownloader(session: RangeAwareOriginProtocol.session()),
            cache: cache
        )
    }

    private func installThenGoOffline(
        bundleID: String = "fixture",
        version: Int = 1,
        offlineError: Error = URLError(.notConnectedToInternet)
    ) async throws -> (cache: AssetBundleCache, manifest: AssetBundleManifest) {
        let manifest = BundleFixture.publish(bundleID: bundleID, version: version, into: source)
        let cache = makeCache()
        let result = await makeService(cache: cache).bundle(accessToken: "t", bundleID: bundleID, progress: nil)
        guard case .success = result else {
            XCTFail("precondition: the bundle should install while online, got \(result)")
            throw XCTSkip("install failed")
        }
        source.fail(bundleID, with: offlineError)
        return (cache, manifest)
    }

    // MARK: - Case 1 — cached content is consumable with no network

    func test_offlineWithAnInstalledBundle_servesTheCachedCopy() async throws {
        let (cache, manifest) = try await installThenGoOffline()

        let recorder = StateRecorder()
        let result = await makeService(cache: cache)
            .bundle(accessToken: "t", bundleID: "fixture", progress: { recorder.record($0) })

        guard case .success(let installed) = result else {
            return XCTFail("a fully installed bundle must open offline, got \(result)")
        }
        XCTAssertEqual(installed.identity, manifest.identity)
        for file in manifest.files {
            let url = try XCTUnwrap(installed.files[file.assetID])
            XCTAssertEqual(Int64(try Data(contentsOf: url).count), file.byteSize)
        }
        XCTAssertFalse(recorder.states.contains { if case .downloading = $0 { return true } else { return false } })
    }

    func test_offlineReadAlsoAppliesToATimeout() async throws {
        let (cache, _) = try await installThenGoOffline(offlineError: URLError(.timedOut))
        let result = await makeService(cache: cache).bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .success = result else {
            return XCTFail("a timeout means the server was not reached, so the cached copy stands: \(result)")
        }
    }

    func test_offlineReadCountsAsAnAccess() async throws {
        let (cache, manifest) = try await installThenGoOffline()
        let entriesBefore = await cache.entries()
        let before = try XCTUnwrap(entriesBefore.first { $0.identity == manifest.identity })

        try await Task.sleep(nanoseconds: 20_000_000)
        _ = await makeService(cache: cache).bundle(accessToken: "t", bundleID: "fixture", progress: nil)

        let entriesAfter = await cache.entries()
        let after = try XCTUnwrap(entriesAfter.first { $0.identity == manifest.identity })
        XCTAssertGreaterThan(after.lastAccessedAt, before.lastAccessedAt)
    }

    // MARK: - Case 2 — uncached content fails with an actionable state

    func test_offlineWithNothingInstalled_reportsOfflineNotAGenericError() async {
        source.fail("never-seen", with: URLError(.notConnectedToInternet))

        let recorder = StateRecorder()
        let result = await makeService(cache: makeCache())
            .bundle(accessToken: "t", bundleID: "never-seen", progress: { recorder.record($0) })

        guard case .failure(let error) = result else {
            return XCTFail("uncached content must never appear available offline, got \(result)")
        }
        XCTAssertEqual(error, .offline,
                       "the distinct case is the point: `.manifestUnreachable` would send the UI to a generic retry message")
        XCTAssertTrue(recorder.states.contains { $0 == .failed(.offline) })
    }

    // MARK: - The restrictive half: a refusal is never satisfied from cache

    func test_serverRefusals_areNeverServedFromCache() async throws {
        for status in [401, 403, 404, 503] {
            let bundleID = "fixture-\(status)"
            let (cache, _) = try await installThenGoOffline(bundleID: bundleID)
            source.fail(bundleID, with: PhotoAPIError(statusCode: status, message: nil, code: nil, assetID: nil))

            let result = await makeService(cache: cache).bundle(accessToken: "t", bundleID: bundleID, progress: nil)
            guard case .failure(let error) = result else {
                return XCTFail("a \(status) must never be satisfied from cache — got \(result)")
            }
            XCTAssertNotEqual(error, .offline, "a \(status) is a server answer, not a connectivity failure")
            switch status {
            case 404: XCTAssertEqual(error, .notPublished)
            case 503: XCTAssertEqual(error, .deliveryUnconfigured)
            default: XCTAssertEqual(error, .manifestUnreachable)
            }
        }
    }

    func test_offlineReadStillVerifiesChecksums() async throws {
        let (cache, manifest) = try await installThenGoOffline()

        let geometryURL = store.installedFileURL(manifest.identity, assetID: "geometry")
        var bytes = try Data(contentsOf: geometryURL)
        bytes[bytes.count / 2] = bytes[bytes.count / 2] == 0x41 ? 0x42 : 0x41
        try bytes.write(to: geometryURL)

        let result = await makeService(cache: cache).bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .failure(let error) = result else {
            return XCTFail("a tampered file must not be served offline, got \(result)")
        }
        XCTAssertEqual(error, .offline, "nothing valid is installed any more, so this is the uncached case")
        let remaining = await cache.entries()
        XCTAssertTrue(remaining.isEmpty, "the corrupt entry should have been removed")
    }

    func test_cancellationDoesNotTriggerTheCachedRead() async throws {
        let (cache, _) = try await installThenGoOffline(offlineError: URLError(.cancelled))
        let result = await makeService(cache: cache).bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .failure(let error) = result else {
            return XCTFail("expected a failure for a cancelled resolution, got \(result)")
        }
        XCTAssertEqual(error, .manifestUnreachable)
        XCTAssertNotEqual(error, .offline)
    }

    // MARK: - Case 4 — reconnect and the manifest is authoritative again

    func test_reconnecting_returnsToManifestAuthority() async throws {
        let (cache, v1) = try await installThenGoOffline()
        let service = makeService(cache: cache)

        guard case .success(let offlineBundle) = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("expected the cached v1 offline")
        }
        XCTAssertEqual(offlineBundle.identity.version, v1.identity.version)

        let v2 = BundleFixture.publish(bundleID: "fixture", version: 2, into: source)
        guard case .success(let reconnected) = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("expected a successful resolution once reachable")
        }
        XCTAssertEqual(reconnected.identity, v2.identity,
                       "online, the manifest decides — a cached v1 must never satisfy a v2 request")
    }

    // MARK: - Case 6 — resume behaviour is unchanged

    func test_offlineAfterAnInterruptedDownload_preservesThePartialAndResumes() async throws {
        let geometry = BundleFixture.geometryBody(version: 1)
        let cut = geometry.count / 3
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source, truncateGeometryAfter: cut)

        let cache = makeCache()
        let service = makeService(cache: cache)
        guard case .failure = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("precondition: a truncated origin should fail the first attempt")
        }
        let partialBytesBefore = store.installedByteCount(
            AssetBundleManifest(identity: AssetBundleIdentity(bundleID: "fixture", version: 1),
                                kind: .roomVariant, format: "usda", minAppVersion: 1, files: [])
        )

        source.fail("fixture", with: URLError(.notConnectedToInternet))
        let offlineResult = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .failure(let error) = offlineResult else {
            return XCTFail("a half-downloaded bundle is not usable content — it must not be served, got \(offlineResult)")
        }
        XCTAssertEqual(error, .offline)

        XCTAssertEqual(
            store.installedByteCount(
                AssetBundleManifest(identity: AssetBundleIdentity(bundleID: "fixture", version: 1),
                                    kind: .roomVariant, format: "usda", minAppVersion: 1, files: [])),
            partialBytesBefore,
            "the offline path must not disturb resume state"
        )

        RangeAwareOriginProtocol.reset()
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        guard case .success = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("expected the resumed download to complete after reconnecting")
        }
        let ranged = RangeAwareOriginProtocol.log.filter { $0.range != nil }
        XCTAssertFalse(ranged.isEmpty, "the retry should have resumed with a Range request, not restarted")
    }

    // MARK: - The authority invariant

    func test_aCachedBundleCarriesNoAuthority() async throws {
        let (cache, _) = try await installThenGoOffline()
        guard case .success(let installed) = await makeService(cache: cache)
            .bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("expected the cached bundle")
        }

        let recordURL = store.entryRecordURL(installed.identity)
        let json = try JSONSerialization.jsonObject(
            with: try Data(contentsOf: recordURL)) as? [String: Any]
        let object = try XCTUnwrap(json)
        XCTAssertEqual(Set(object.keys), [
            "format_version", "bundle_id", "version", "kind", "format",
            "files", "total_bytes", "installed_at", "last_accessed_at"
        ], "an offline-servable cache entry must describe a bundle and nothing else")

        let files = try XCTUnwrap(object["files"] as? [[String: Any]])
        XCTAssertFalse(files.isEmpty)
        for file in files {
            XCTAssertEqual(Set(file.keys), ["asset_id", "role", "byte_size", "checksum_sha256"])
        }
        XCTAssertEqual(object["bundle_id"] as? String, "fixture")
    }
}
