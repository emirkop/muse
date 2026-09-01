import CryptoKit
import Foundation
import XCTest
@testable import MuseApp

// MARK: - A range-aware origin, in-process

final class RangeAwareOriginProtocol: URLProtocol {
    struct Behaviour: Sendable {
        var body: Data
        var truncateAfter: Int?
        var ignoresRange = false
        var status: Int?
    }

    private static let lock = NSLock()
    nonisolated(unsafe) private static var routes: [String: Behaviour] = [:]
    nonisolated(unsafe) private static var requestLog: [(url: String, range: String?)] = []

    static func install(_ behaviour: Behaviour, at url: URL) {
        lock.lock()
        routes[url.absoluteString] = behaviour
        lock.unlock()
    }

    static func reset() {
        lock.lock()
        routes = [:]
        requestLog = []
        lock.unlock()
    }

    static var log: [(url: String, range: String?)] {
        lock.lock()
        defer { lock.unlock() }
        return requestLog
    }

    static func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RangeAwareOriginProtocol.self]
        configuration.urlCache = nil
        return URLSession(configuration: configuration)
    }

    override class func canInit(with request: URLRequest) -> Bool {
        guard let url = request.url?.absoluteString else { return false }
        lock.lock()
        defer { lock.unlock() }
        return routes[url] != nil
    }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let url = request.url else { return }
        Self.lock.lock()
        let behaviour = Self.routes[url.absoluteString]
        Self.requestLog.append((url.absoluteString, request.value(forHTTPHeaderField: "Range")))
        Self.lock.unlock()

        guard let behaviour else {
            client?.urlProtocol(self, didFailWithError: URLError(.badURL))
            return
        }
        if let status = behaviour.status {
            respond(url: url, status: status, headers: [:], body: Data())
            return
        }

        let total = behaviour.body.count
        var start = 0
        var status = 200
        var headers = ["Content-Type": "application/octet-stream"]

        if let rangeHeader = request.value(forHTTPHeaderField: "Range"), !behaviour.ignoresRange {
            guard let parsed = Self.parseOpenEndedRange(rangeHeader), parsed < total else {
                respond(url: url, status: 416, headers: ["Content-Range": "bytes */\(total)"], body: Data())
                return
            }
            start = parsed
            status = 206
            headers["Content-Range"] = "bytes \(start)-\(total - 1)/\(total)"
        }

        var slice = behaviour.body.subdata(in: start..<total)
        var fail = false
        if let cut = behaviour.truncateAfter, cut < slice.count {
            slice = slice.subdata(in: 0..<cut)
            fail = true
        }
        headers["Content-Length"] = String(slice.count)

        respond(url: url, status: status, headers: headers, body: slice, thenFail: fail)
    }

    override func stopLoading() {}

    private func respond(url: URL, status: Int, headers: [String: String], body: Data, thenFail: Bool = false) {
        let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers)!
        let queue = DispatchQueue(label: "range-aware-origin")
        queue.async { [weak self] in
            guard let self else { return }
            self.client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)

            var offset = 0
            let chunkSize = 1024
            while offset < body.count {
                let end = min(offset + chunkSize, body.count)
                let chunk = body.subdata(in: offset..<end)
                offset = end
                self.client?.urlProtocol(self, didLoad: chunk)
                Thread.sleep(forTimeInterval: 0.001)
            }

            if thenFail {
                self.client?.urlProtocol(self, didFailWithError: URLError(.networkConnectionLost))
            } else {
                self.client?.urlProtocolDidFinishLoading(self)
            }
        }
    }

    static func parseOpenEndedRange(_ header: String) -> Int? {
        guard header.hasPrefix("bytes=") else { return nil }
        let spec = header.dropFirst("bytes=".count)
        guard spec.hasSuffix("-"), let start = Int(spec.dropLast()) else { return nil }
        return start
    }
}

// MARK: - Fakes and fixtures

final class StubManifestSource: AssetBundleManifestFetching, @unchecked Sendable {
    private let lock = NSLock()
    private var manifests: [String: AssetBundleManifest] = [:]
    private var failures: [String: Error] = [:]
    private(set) var fetchCount = 0

    func publish(_ manifest: AssetBundleManifest) {
        lock.lock()
        manifests[manifest.bundleID] = manifest
        failures[manifest.bundleID] = nil
        lock.unlock()
    }

    func fail(_ bundleID: String, with error: Error) {
        lock.lock()
        failures[bundleID] = error
        lock.unlock()
    }

    func manifest(accessToken: String, bundleID: String, appAssetVersion: Int) async throws -> AssetBundleManifest {
        let (failure, manifest) = take(bundleID)
        if let failure { throw failure }
        guard let manifest else {
            throw PhotoAPIError(statusCode: 404, message: "asset bundle not found", code: nil, assetID: nil)
        }
        return manifest
    }

    private func take(_ bundleID: String) -> (Error?, AssetBundleManifest?) {
        lock.lock()
        defer { lock.unlock() }
        fetchCount += 1
        return (failures[bundleID], manifests[bundleID])
    }
}

enum BundleFixture {
    static func sha256Hex(_ data: Data) -> String {
        SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    static func geometryBody(version: Int, lines: Int = 400) -> Data {
        var text = "#usda 1.0\n(\n    doc = \"MUSE DEVELOPMENT FIXTURE v\(version) - NOT production artwork\"\n)\n"
        for index in 0..<lines {
            text += "# fixture v\(version) filler line \(String(format: "%04d", index))\n"
        }
        return Data(text.utf8)
    }

    static func layoutBody(variantID: String, version: Int) -> Data {
        let json = """
        {
          "format_version": 1,
          "variant_id": "\(variantID)",
          "entry": { "position": [0.0, 0.0, 3.5], "yaw": 0.0 },
          "photo_transforms": [
            { "wall": "focal", "position_on_wall": 0,
              "position": [0.0, 1.55, -4.415], "rotation": [0.0, 0.0, 0.0, 1.0], "scale": [1.6, 1.2, 1.0] },
            { "wall": "left", "position_on_wall": 0,
              "position": [-3.415, 1.55, 3.877], "rotation": [0.0, 0.7071068, 0.0, 0.7071068], "scale": [0.55, 0.55, 1.0] },
            { "wall": "right", "position_on_wall": 0,
              "position": [3.415, 1.55, 3.877], "rotation": [0.0, -0.7071068, 0.0, 0.7071068], "scale": [0.55, 0.55, 1.0] },
            { "wall": "rear", "position_on_wall": 0,
              "position": [0.0, 1.55, 4.415], "rotation": [0.0, 1.0, 0.0, 0.0], "scale": [1.6, 1.2, 1.0] }
          ],
          "sculpture_transforms": [
            { "slot_index": 0, "position": [-2.4, 0.0, -3.4], "rotation": [0,0,0,1], "scale": [0.7, 1.4, 0.7] }
          ],
          "version_marker": \(version)
        }
        """
        return Data(json.utf8)
    }

    @discardableResult
    static func publish(
        bundleID: String,
        version: Int,
        variantID: String = "dev-fixture:room-variant",
        into source: StubManifestSource,
        truncateGeometryAfter: Int? = nil,
        originIgnoresRange: Bool = false,
        corruptGeometryBytes: Bool = false
    ) -> AssetBundleManifest {
        let geometry = geometryBody(version: version)
        let layout = layoutBody(variantID: variantID, version: version)

        let geometryURL = URL(string: "https://assets.example/bundles/\(bundleID)/v\(version)/geometry")!
        let layoutURL = URL(string: "https://assets.example/bundles/\(bundleID)/v\(version)/layout")!

        let servedGeometry = corruptGeometryBytes ? geometry + Data("tampered".utf8) : geometry

        RangeAwareOriginProtocol.install(
            .init(body: servedGeometry, truncateAfter: truncateGeometryAfter, ignoresRange: originIgnoresRange),
            at: geometryURL
        )
        RangeAwareOriginProtocol.install(.init(body: layout), at: layoutURL)

        let manifest = AssetBundleManifest(
            identity: AssetBundleIdentity(bundleID: bundleID, version: version),
            kind: .roomVariant,
            format: "usda",
            minAppVersion: 1,
            files: [
                AssetBundleFile(assetID: "geometry", role: .geometry, url: geometryURL,
                                contentType: "model/vnd.usda+ascii",
                                byteSize: Int64(geometry.count), checksumSHA256: sha256Hex(geometry)),
                AssetBundleFile(assetID: "layout", role: .layout, url: layoutURL,
                                contentType: "application/json",
                                byteSize: Int64(layout.count), checksumSHA256: sha256Hex(layout))
            ]
        )
        source.publish(manifest)
        return manifest
    }
}

// MARK: - Tests

final class AssetBundleDeliveryTests: XCTestCase {
    private var root: URL!
    private var store: AssetBundleStore!
    private var source: StubManifestSource!
    private var registry: ActiveBundleRegistry!

    override func setUp() {
        super.setUp()
        URLProtocol.registerClass(RangeAwareOriginProtocol.self)
        RangeAwareOriginProtocol.reset()
        root = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("MuseAssetBundleTests-\(UUID().uuidString)", isDirectory: true)
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

    private func makeService() -> AssetBundleDeliveryService {
        AssetBundleDeliveryService(
            manifests: source,
            downloader: URLSessionAssetBundleDownloader(session: RangeAwareOriginProtocol.session()),
            cache: AssetBundleCache(store: store, policy: .developmentDefault, retention: registry)
        )
    }

    // MARK: PROOF 1 — a published fixture downloads, verifies, and installs

    func test_publishedBundle_downloadsVerifiesAndInstalls() async throws {
        let manifest = BundleFixture.publish(bundleID: "fixture", version: 1, into: source)

        let recorder = StateRecorder()
        let result = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: { recorder.record($0) })

        guard case .success(let installed) = result else {
            return XCTFail("expected success, got \(result)")
        }
        XCTAssertEqual(installed.identity, manifest.identity)
        XCTAssertEqual(installed.format, "usda")

        for file in manifest.files {
            let url = try XCTUnwrap(installed.files[file.assetID])
            let data = try Data(contentsOf: url)
            XCTAssertEqual(Int64(data.count), file.byteSize, "\(file.assetID) length")
            XCTAssertEqual(BundleFixture.sha256Hex(data), file.checksumSHA256, "\(file.assetID) digest")
        }

        let observed = recorder.states
        XCTAssertTrue(observed.contains { if case .geometryReady = $0 { return true } else { return false } },
                      "expected a geometryReady state, got \(observed)")
        XCTAssertTrue(observed.contains { if case .installed = $0 { return true } else { return false } })
    }

    func test_geometryIsFetchedBeforeTheLayout() async throws {
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        _ = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)

        let order = RangeAwareOriginProtocol.log.map(\.url)
        let geometryIndex = try XCTUnwrap(order.firstIndex { $0.hasSuffix("/geometry") })
        let layoutIndex = try XCTUnwrap(order.firstIndex { $0.hasSuffix("/layout") })
        XCTAssertLessThan(geometryIndex, layoutIndex, "geometry must be fetched first — `02`'s progressive reveal")
    }

    func test_alreadyInstalledBundle_downloadsNothing() async throws {
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let service = makeService()
        _ = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        let firstPassRequests = RangeAwareOriginProtocol.log.count

        let result = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .success = result else { return XCTFail("expected success") }
        XCTAssertEqual(RangeAwareOriginProtocol.log.count, firstPassRequests,
                       "a second resolution of an installed version must fetch no bytes")
    }

    // MARK: PROOF 2 — an interrupted download RESUMES rather than restarting

    func test_interruptedDownload_resumesFromWhereItStopped() async throws {
        let geometry = BundleFixture.geometryBody(version: 1)
        let cut = geometry.count / 3

        let manifest = BundleFixture.publish(
            bundleID: "fixture", version: 1, into: source, truncateGeometryAfter: cut
        )
        let firstResult = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .failure(.downloadFailed) = firstResult else {
            return XCTFail("expected the interrupted attempt to fail, got \(firstResult)")
        }

        let partial = store.partialFileURL(manifest.identity, assetID: "geometry")
        XCTAssertEqual(URLSessionAssetBundleDownloader.byteCount(at: partial), Int64(cut),
                       "the partial must hold exactly the bytes that arrived")
        XCTAssertFalse(FileManager.default.fileExists(
            atPath: store.installedFileURL(manifest.identity, assetID: "geometry").path),
            "an unverified partial must never be installed")

        RangeAwareOriginProtocol.reset()
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let result = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .success(let installed) = result else {
            return XCTFail("expected success on resume, got \(result)")
        }

        let data = try Data(contentsOf: try XCTUnwrap(installed.geometryURL))
        XCTAssertEqual(data, geometry, "the resumed file must reconstruct the original exactly")

        let geometryRequests = RangeAwareOriginProtocol.log.filter { $0.url.hasSuffix("/geometry") }
        XCTAssertEqual(geometryRequests.count, 1)
        XCTAssertEqual(geometryRequests.first?.range, "bytes=\(cut)-",
                       "expected a range request from the resume offset, not a fresh download")
    }

    func test_completedButUninstalledPartial_isVerifiedNotRefetched() async throws {
        let manifest = BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let geometry = BundleFixture.geometryBody(version: 1)
        let partial = store.partialFileURL(manifest.identity, assetID: "geometry")
        try FileManager.default.createDirectory(at: partial.deletingLastPathComponent(), withIntermediateDirectories: true)
        try geometry.write(to: partial)

        _ = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)

        XCTAssertTrue(RangeAwareOriginProtocol.log.filter { $0.url.hasSuffix("/geometry") }.isEmpty,
                      "a complete partial must be verified and installed, never re-downloaded")
    }

    func test_originThatIgnoresRange_restartsInsteadOfCorruptingThePartial() async throws {
        let geometry = BundleFixture.geometryBody(version: 1)
        let cut = geometry.count / 2

        let manifest = BundleFixture.publish(
            bundleID: "fixture", version: 1, into: source, truncateGeometryAfter: cut
        )
        _ = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        XCTAssertEqual(URLSessionAssetBundleDownloader.byteCount(at: store.partialFileURL(manifest.identity, assetID: "geometry")), Int64(cut))

        RangeAwareOriginProtocol.reset()
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source, originIgnoresRange: true)

        let result = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .success(let installed) = result else {
            return XCTFail("expected success after restarting, got \(result)")
        }
        let data = try Data(contentsOf: try XCTUnwrap(installed.geometryURL))
        XCTAssertEqual(data, geometry, "a range-ignoring origin must cause a restart, never an append")
    }

    func test_oversizedPartial_isDiscardedRatherThanTruncated() async throws {
        let manifest = BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let partial = store.partialFileURL(manifest.identity, assetID: "geometry")
        try FileManager.default.createDirectory(at: partial.deletingLastPathComponent(), withIntermediateDirectories: true)
        try (BundleFixture.geometryBody(version: 1) + Data(repeating: 0x41, count: 500)).write(to: partial)

        let result = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .success(let installed) = result else {
            return XCTFail("expected success, got \(result)")
        }
        XCTAssertEqual(try Data(contentsOf: try XCTUnwrap(installed.geometryURL)),
                       BundleFixture.geometryBody(version: 1))
        let requests = RangeAwareOriginProtocol.log.filter { $0.url.hasSuffix("/geometry") }
        XCTAssertEqual(requests.first?.range, nil, "an oversized partial must be discarded and the file fetched whole")
    }

    // MARK: PROOF 3 — corruption is rejected

    func test_bytesThatDoNotMatchTheChecksum_areRejectedAndDiscarded() async throws {
        let manifest = BundleFixture.publish(
            bundleID: "fixture", version: 1, into: source, corruptGeometryBytes: true
        )

        let result = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .failure(.integrityCheckFailed(let assetID)) = result else {
            return XCTFail("expected an integrity failure, got \(result)")
        }
        XCTAssertEqual(assetID, "geometry")

        XCTAssertFalse(FileManager.default.fileExists(
            atPath: store.installedFileURL(manifest.identity, assetID: "geometry").path))
        XCTAssertEqual(URLSessionAssetBundleDownloader.byteCount(at: store.partialFileURL(manifest.identity, assetID: "geometry")), 0)
    }

    func test_correctLengthButWrongBytes_isRejected() async throws {
        let manifest = BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let file = try XCTUnwrap(manifest.geometryFile)

        let partial = store.partialFileURL(manifest.identity, assetID: "geometry")
        try FileManager.default.createDirectory(at: partial.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(repeating: 0x42, count: Int(file.byteSize)).write(to: partial)

        let result = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .failure(.integrityCheckFailed) = result else {
            return XCTFail("expected an integrity failure for a right-length wrong-bytes file, got \(result)")
        }
    }

    // MARK: PROOF 4 — a new version supersedes the old one

    func test_publishingV2_makesV1StaleAndFetchesV2() async throws {
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let service = makeService()
        guard case .success(let first) = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("v1 should install")
        }
        XCTAssertEqual(first.identity.version, 1)
        var versions = await service.installedVersions(ofBundle: "fixture")
        XCTAssertEqual(versions, [1])

        BundleFixture.publish(bundleID: "fixture", version: 2, into: source)

        guard case .success(let second) = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("v2 should install")
        }
        XCTAssertEqual(second.identity.version, 2, "the client must pick up the new version by itself")
        let data = try Data(contentsOf: try XCTUnwrap(second.geometryURL))
        XCTAssertEqual(data, BundleFixture.geometryBody(version: 2), "v2's bytes, not v1's")

        versions = await service.installedVersions(ofBundle: "fixture")
        XCTAssertEqual(versions, [2])
    }

    // MARK: PROOF 5 — stale cannot masquerade as current

    func test_installedV1IsNeverServedOnceV2IsPublished() async throws {
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let service = makeService()
        _ = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil)

        BundleFixture.publish(bundleID: "fixture", version: 2, into: source)
        for _ in 0..<3 {
            guard case .success(let installed) = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
                return XCTFail("expected success")
            }
            XCTAssertEqual(installed.identity.version, 2)
            XCTAssertTrue(installed.geometryURL?.path.contains("/v2/") == true,
                          "the served file must come from the v2 directory")
        }
    }

    func test_withoutAManifest_anInstalledBundleIsNotServed() async throws {
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let service = makeService()
        guard case .success = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("v1 should install")
        }

        source.fail("fixture", with: IdentityAPIClientError.transport)
        let result = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .failure(.manifestUnreachable) = result else {
            return XCTFail("expected manifestUnreachable, got \(result)")
        }
        var versions = await service.installedVersions(ofBundle: "fixture")
        XCTAssertEqual(versions, [1])
    }

    func test_everyResolutionFetchesAManifest() async throws {
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let service = makeService()
        for _ in 0..<3 {
            _ = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        }
        XCTAssertEqual(source.fetchCount, 3, "the manifest is the only authority on what is current")
    }

    // MARK: Honest failure states

    func test_nothingPublished_reportsNotPublished() async {
        let result = await makeService().bundle(accessToken: "t", bundleID: "bundle_style_modern", progress: nil)
        guard case .failure(.notPublished) = result else {
            return XCTFail("expected notPublished — the honest state of every production Style today, got \(result)")
        }
    }

    func test_deploymentWithoutBundleDelivery_reportsUnconfigured() async {
        source.fail("fixture", with: PhotoAPIError(statusCode: 503, message: nil, code: nil, assetID: nil))
        let result = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .failure(.deliveryUnconfigured) = result else {
            return XCTFail("expected deliveryUnconfigured, got \(result)")
        }
    }

    // MARK: PROOF 6 — asset identity carries no user content

    func test_installPathIsAddressedOnlyByBundleIdentityAndAsset() async throws {
        BundleFixture.publish(bundleID: "fixture", version: 7, into: source)
        guard case .success(let installed) = await makeService().bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("expected success")
        }
        for (assetID, url) in installed.files {
            let path = url.path
            XCTAssertTrue(path.contains("/fixture/v7/\(assetID)"),
                          "an installed file's path must name only bundle, version and asset: \(path)")
        }
        let contents = try FileManager.default
            .subpathsOfDirectory(atPath: root.path)
            .joined(separator: "\n")
        for forbidden in ["account", "room", "museum", "photo", "user"] {
            XCTAssertFalse(contents.lowercased().contains(forbidden),
                           "the install tree references \(forbidden): \(contents)")
        }
    }

    // MARK: Concurrency

    func test_concurrentResolutionsOfOneBundleDownloadItOnce() async throws {
        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let service = makeService()

        await withTaskGroup(of: Void.self) { group in
            for _ in 0..<6 {
                group.addTask {
                    _ = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil)
                }
            }
        }
        let geometryRequests = RangeAwareOriginProtocol.log.filter { $0.url.hasSuffix("/geometry") }
        XCTAssertEqual(geometryRequests.count, 1, "six callers must share one download, not race six")
    }
}

final class StateRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var collected: [AssetBundleDeliveryState] = []

    func record(_ state: AssetBundleDeliveryState) {
        lock.lock()
        collected.append(state)
        lock.unlock()
    }

    var states: [AssetBundleDeliveryState] {
        lock.lock()
        defer { lock.unlock() }
        return collected
    }
}
