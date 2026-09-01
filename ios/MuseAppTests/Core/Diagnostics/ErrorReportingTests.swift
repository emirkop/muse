import XCTest
@testable import MuseApp

final class ErrorReportingTests: XCTestCase {
    private var root: URL!
    private var store: AssetBundleStore!
    private var source: StubManifestSource!
    private var registry: ActiveBundleRegistry!

    override func setUp() {
        super.setUp()
        URLProtocol.registerClass(RangeAwareOriginProtocol.self)
        RangeAwareOriginProtocol.reset()
        root = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("MuseObservability-\(UUID().uuidString)", isDirectory: true)
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

    private func makeService(_ reporter: any ErrorReporting) -> AssetBundleDeliveryService {
        AssetBundleDeliveryService(
            manifests: source,
            downloader: URLSessionAssetBundleDownloader(session: RangeAwareOriginProtocol.session()),
            cache: AssetBundleCache(store: store, policy: .developmentDefault,
                                    retention: registry, diagnostics: reporter),
            diagnostics: reporter
        )
    }

    // MARK: - An asset-load failure reaches the abstraction

    func test_anAssetLoadFailureIsReported() async {
        let reporter = RecordingReporter()
        _ = await makeService(reporter).bundle(accessToken: "t", bundleID: "never-published", progress: nil)

        let reports = reporter.reports
        XCTAssertEqual(reports.count, 1, "expected exactly one report, got \(reports)")
        XCTAssertEqual(reports.first?.domain, .assetDelivery)
        XCTAssertEqual(reports.first?.reason, .notPublished)
        XCTAssertEqual(reports.first?.bundle, "never-published")
    }

    func test_everyDeliveryFailureMapsToADeliberateReason() {
        let expected: [(AssetBundleDeliveryError, ErrorReport.Reason)] = [
            (.offline, .offline),
            (.manifestUnreachable, .unreachable),
            (.downloadFailed, .unreachable),
            (.notPublished, .notPublished),
            (.deliveryUnconfigured, .refused),
            (.integrityCheckFailed(assetID: "geometry"), .integrityFailed),
            (.storageFailed, .storageFailed),
            (.malformedBundle, .malformedBundle),
        ]
        for (error, reason) in expected {
            XCTAssertEqual(ErrorReport.Reason(deliveryError: error), reason, "\(error)")
        }
    }

    func test_aCacheIntegrityFailureIsReported() async throws {
        let reporter = RecordingReporter()
        let manifest = BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let service = makeService(reporter)
        guard case .success = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil) else {
            return XCTFail("precondition: the fixture should install")
        }
        reporter.clear()

        let geometryURL = store.installedFileURL(manifest.identity, assetID: "geometry")
        var bytes = try Data(contentsOf: geometryURL)
        bytes[bytes.count / 2] = bytes[bytes.count / 2] == 0x41 ? 0x42 : 0x41
        try bytes.write(to: geometryURL)

        _ = await service.bundle(accessToken: "t", bundleID: "fixture", progress: nil)

        let integrity = reporter.reports.filter { $0.domain == .assetCache }
        XCTAssertEqual(integrity.count, 1, "expected one cache-integrity report, got \(reporter.reports)")
        XCTAssertEqual(integrity.first?.reason, .integrityFailed)
        XCTAssertEqual(integrity.first?.bundle, "fixture@1")
    }

    // MARK: - Reporting can never affect product behaviour

    func test_aReportingFailureDoesNotAffectDelivery() async {
        let exploding = ExplodingReporter()
        let quiet = RecordingReporter()

        BundleFixture.publish(bundleID: "fixture", version: 1, into: source)
        let withExploding = await makeService(exploding).bundle(accessToken: "t", bundleID: "fixture", progress: nil)
        guard case .success(let bundle) = withExploding else {
            return XCTFail("a failing reporter must not affect a successful delivery: \(withExploding)")
        }
        XCTAssertEqual(bundle.identity.version, 1)

        let failing = await makeService(exploding).bundle(accessToken: "t", bundleID: "never-published", progress: nil)
        let control = await makeService(quiet).bundle(accessToken: "t", bundleID: "never-published", progress: nil)
        guard case .failure(let a) = failing, case .failure(let b) = control else {
            return XCTFail("both should fail")
        }
        XCTAssertEqual(a, b, "the outcome must not depend on whether reporting worked")
        XCTAssertTrue(exploding.wasCalled, "the exploding reporter was never exercised, so this proved nothing")
    }

    func test_reportingIsFireAndForgetBySignature() {
        let reporter: any ErrorReporting = NoErrorReporting()
        reporter.report(ErrorReport(domain: .runtimeScene, reason: .sceneLoadFailed))
    }

    // MARK: - Sensitive-data policy

    func test_aReportCanCarryNothingSensitive() throws {
        let report = ErrorReport(domain: .assetDelivery, reason: .integrityFailed, bundle: "style_a@3")
        let mirror = Mirror(reflecting: report)
        XCTAssertEqual(Set(mirror.children.compactMap(\.label)), ["domain", "reason", "bundle"])

        XCTAssertEqual(report.bundle, "style_a@3")
    }

    func test_noThirdPartyCrashReporterExists() throws {
        let projectRoot = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
        let modules = ["Sentry", "Bugsnag", "FirebaseCrashlytics", "Crashlytics",
                       "Instabug", "Datadog", "NewRelic", "Rollbar", "Embrace"]

        var scanned = 0
        for target in ["MuseApp", "project.yml"] {
            let url = projectRoot.appendingPathComponent(target)
            guard let enumerator = FileManager.default.enumerator(at: url, includingPropertiesForKeys: nil) else {
                if let source = try? String(contentsOf: url, encoding: .utf8) {
                    scanned += 1
                    for module in modules {
                        XCTAssertFalse(source.contains(module), "project.yml references \(module)")
                    }
                }
                continue
            }
            for case let file as URL in enumerator where file.pathExtension == "swift" {
                guard let source = try? String(contentsOf: file, encoding: .utf8) else { continue }
                scanned += 1
                for module in modules {
                    XCTAssertFalse(source.contains("import \(module)"),
                                   "\(file.lastPathComponent) imports \(module); `04` Part K leaves provider selection open")
                }
            }
        }
        XCTAssertGreaterThan(scanned, 50, "scanned only \(scanned) files — the check proved nothing")
    }
}

// MARK: - Support

final class RecordingReporter: ErrorReporting, @unchecked Sendable {
    private let lock = NSLock()
    private var stored: [ErrorReport] = []

    var reports: [ErrorReport] {
        lock.lock(); defer { lock.unlock() }
        return stored
    }

    func clear() {
        lock.lock(); stored.removeAll(); lock.unlock()
    }

    func report(_ report: ErrorReport) {
        lock.lock(); stored.append(report); lock.unlock()
    }
}

final class ExplodingReporter: ErrorReporting, @unchecked Sendable {
    private let lock = NSLock()
    private var called = false

    var wasCalled: Bool {
        lock.lock(); defer { lock.unlock() }
        return called
    }

    func report(_ report: ErrorReport) {
        lock.lock(); called = true; lock.unlock()
        _ = try? Data(contentsOf: URL(fileURLWithPath: "/nonexistent/diagnostics/sink"))
    }
}
