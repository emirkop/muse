import Foundation

public actor AssetBundleCache {
    public nonisolated let store: AssetBundleStore
    public nonisolated let policy: AssetCachePolicy
    private let retention: any AssetBundleRetaining
    private let now: @Sendable () -> Date
    private let diagnostics: any ErrorReporting

    public init(
        store: AssetBundleStore = AssetBundleStore(),
        policy: AssetCachePolicy = .developmentDefault,
        retention: any AssetBundleRetaining,
        now: @escaping @Sendable () -> Date = { Date() },
        diagnostics: any ErrorReporting = NoErrorReporting()
    ) {
        self.store = store
        self.policy = policy
        self.retention = retention
        self.now = now
        self.diagnostics = diagnostics
    }

    // MARK: - Hits

    public func hit(_ manifest: AssetBundleManifest) -> InstalledAssetBundle? {
        guard var record = readRecord(manifest.identity) else {
            return nil
        }
        guard record.matches(manifest), verifyFiles(of: record) else {
            diagnostics.report(ErrorReport(
                domain: .assetCache, reason: .integrityFailed,
                bundle: "\(manifest.bundleID)@\(manifest.version)"
            ))
            store.removeVersion(manifest.identity)
            return nil
        }

        record.lastAccessedAt = now().timeIntervalSince1970
        writeRecord(record)
        return record.installedBundle(in: store)
    }

    public func offlineEntry(bundleID: String) -> InstalledAssetBundle? {
        let candidates = store.recordedVersions(ofBundle: bundleID).sorted(by: >)
        for version in candidates {
            let identity = AssetBundleIdentity(bundleID: bundleID, version: version)
            guard var record = readRecord(identity) else { continue }
            guard verifyFiles(of: record) else {
                diagnostics.report(ErrorReport(
                    domain: .assetCache, reason: .integrityFailed,
                    bundle: "\(bundleID)@\(version)"
                ))
                store.removeVersion(identity)
                continue
            }
            record.lastAccessedAt = now().timeIntervalSince1970
            writeRecord(record)
            return record.installedBundle(in: store)
        }
        return nil
    }

    // MARK: - Commits

    public enum CommitFailure: Error, Equatable {
        case incompleteInstall(assetID: String)
        case recordUnwritable
    }

    public func commit(_ manifest: AssetBundleManifest) throws -> (bundle: InstalledAssetBundle, eviction: EvictionReport) {
        for file in manifest.files {
            let url = store.installedFileURL(manifest.identity, assetID: file.assetID)
            guard URLSessionAssetBundleDownloader.byteCount(at: url) == file.byteSize,
                  FileManager.default.fileExists(atPath: url.path) else {
                throw CommitFailure.incompleteInstall(assetID: file.assetID)
            }
        }

        let timestamp = now().timeIntervalSince1970
        let record = CacheEntryRecord(manifest: manifest, installedAt: timestamp, lastAccessedAt: timestamp)
        guard writeRecord(record) else { throw CommitFailure.recordUnwritable }

        store.removeVersions(ofBundle: manifest.bundleID, keeping: manifest.version)
        let report = enforceBudget(protecting: [manifest.identity])
        return (record.installedBundle(in: store), report)
    }

    // MARK: - Eviction

    public struct EvictionReport: Equatable, Sendable {
        public var evicted: [AssetBundleIdentity] = []
        public var bytesFreed: Int64 = 0
        public var remainingOverBudget: Int64 = 0
        public var totalBytesAfter: Int64 = 0
    }

    @discardableResult
    public func enforceBudget(protecting extra: Set<AssetBundleIdentity> = []) -> EvictionReport {
        enforceBudget(on: allValidRecords(), protecting: extra)
    }

    @discardableResult
    private func enforceBudget(
        on records: [CacheEntryRecord],
        protecting extra: Set<AssetBundleIdentity> = []
    ) -> EvictionReport {
        var entries = records
        var report = EvictionReport()
        var total = entries.reduce(0) { $0 + $1.totalBytes }

        guard total > policy.budgetBytes else {
            report.totalBytesAfter = total
            return report
        }

        let active = retention.activeIdentities
        var candidates = entries
            .filter { !active.contains($0.identity) && !extra.contains($0.identity) }
            .sorted(by: CacheEntryRecord.leastRecentlyUsedFirst)

        while total > policy.budgetBytes, !candidates.isEmpty {
            let victim = candidates.removeFirst()
            store.removeVersion(victim.identity)
            entries.removeAll { $0.identity == victim.identity }
            total -= victim.totalBytes
            report.evicted.append(victim.identity)
            report.bytesFreed += victim.totalBytes
        }

        report.remainingOverBudget = max(0, total - policy.budgetBytes)
        report.totalBytesAfter = total
        return report
    }

    // MARK: - Reconciliation

    public struct ReconcileReport: Equatable, Sendable {
        public var garbageRemoved: [AssetBundleIdentity] = []
        public var validEntries: Int = 0
        public var eviction = EvictionReport()
    }

    @discardableResult
    public func reconcile() -> ReconcileReport {
        var report = ReconcileReport()
        var valid: [CacheEntryRecord] = []

        for bundleID in store.bundleIDsOnDisk() {
            let directories = Set(store.installedVersions(ofBundle: bundleID))
            let records = Set(store.recordedVersions(ofBundle: bundleID))

            for version in directories.union(records).sorted() {
                let identity = AssetBundleIdentity(bundleID: bundleID, version: version)
                guard let record = readRecord(identity), directories.contains(version), filesPresent(of: record) else {
                    store.removeIncompleteInstall(identity)
                    report.garbageRemoved.append(identity)
                    continue
                }
                report.validEntries += 1
                valid.append(record)
            }
        }

        report.eviction = enforceBudget(on: valid)
        return report
    }

    // MARK: - Queries

    public func entries() -> [CacheEntry] {
        allValidRecords()
            .sorted(by: CacheEntryRecord.leastRecentlyUsedFirst)
            .map { CacheEntry(identity: $0.identity, totalBytes: $0.totalBytes,
                              installedAt: Date(timeIntervalSince1970: $0.installedAt),
                              lastAccessedAt: Date(timeIntervalSince1970: $0.lastAccessedAt)) }
    }

    public func totalBytes() -> Int64 {
        allValidRecords().reduce(0) { $0 + $1.totalBytes }
    }

    public struct CacheEntry: Equatable, Sendable {
        public let identity: AssetBundleIdentity
        public let totalBytes: Int64
        public let installedAt: Date
        public let lastAccessedAt: Date
    }

    // MARK: - Records

    private func allValidRecords() -> [CacheEntryRecord] {
        var records: [CacheEntryRecord] = []
        for bundleID in store.bundleIDsOnDisk() {
            for version in store.recordedVersions(ofBundle: bundleID) {
                let identity = AssetBundleIdentity(bundleID: bundleID, version: version)
                if let record = readRecord(identity),
                   FileManager.default.fileExists(atPath: store.versionDirectory(identity).path) {
                    records.append(record)
                }
            }
        }
        return records
    }

    private let recordDecoder = JSONDecoder()
    private let recordEncoder = JSONEncoder()

    private func readRecord(_ identity: AssetBundleIdentity) -> CacheEntryRecord? {
        guard let data = try? Data(contentsOf: store.entryRecordURL(identity)),
              let record = try? recordDecoder.decode(CacheEntryRecord.self, from: data),
              record.formatVersion == CacheEntryRecord.currentFormatVersion,
              record.identity == identity else {
            return nil
        }
        return record
    }

    @discardableResult
    private func writeRecord(_ record: CacheEntryRecord) -> Bool {
        guard let data = try? recordEncoder.encode(record) else { return false }
        let url = store.entryRecordURL(record.identity)
        do {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try data.write(to: url, options: .atomic)
            return true
        } catch {
            return false
        }
    }

    private func filesPresent(of record: CacheEntryRecord) -> Bool {
        record.files.allSatisfy { file in
            let url = store.installedFileURL(record.identity, assetID: file.assetID)
            return FileManager.default.fileExists(atPath: url.path)
                && URLSessionAssetBundleDownloader.byteCount(at: url) == file.byteSize
        }
    }

    private func verifyFiles(of record: CacheEntryRecord) -> Bool {
        guard filesPresent(of: record) else { return false }
        return record.files.allSatisfy { file in
            let url = store.installedFileURL(record.identity, assetID: file.assetID)
            guard let digest = try? URLSessionAssetBundleDownloader.sha256Hex(of: url) else { return false }
            return digest == file.checksumSHA256
        }
    }
}

// MARK: - The record

struct CacheEntryRecord: Codable, Equatable {
    static let currentFormatVersion = 1

    struct File: Codable, Equatable {
        let assetID: String
        let role: String
        let byteSize: Int64
        let checksumSHA256: String

        enum CodingKeys: String, CodingKey {
            case assetID = "asset_id"
            case role
            case byteSize = "byte_size"
            case checksumSHA256 = "checksum_sha256"
        }
    }

    let formatVersion: Int
    let bundleID: String
    let version: Int
    let kind: String
    let format: String
    let files: [File]
    let totalBytes: Int64
    let installedAt: TimeInterval
    var lastAccessedAt: TimeInterval

    enum CodingKeys: String, CodingKey {
        case formatVersion = "format_version"
        case bundleID = "bundle_id"
        case version
        case kind
        case format
        case files
        case totalBytes = "total_bytes"
        case installedAt = "installed_at"
        case lastAccessedAt = "last_accessed_at"
    }

    init(manifest: AssetBundleManifest, installedAt: TimeInterval, lastAccessedAt: TimeInterval) {
        formatVersion = Self.currentFormatVersion
        bundleID = manifest.bundleID
        version = manifest.version
        kind = manifest.kind.rawValue
        format = manifest.format
        files = manifest.files.map {
            File(assetID: $0.assetID, role: $0.role.rawValue, byteSize: $0.byteSize, checksumSHA256: $0.checksumSHA256)
        }
        totalBytes = manifest.totalByteSize
        self.installedAt = installedAt
        self.lastAccessedAt = lastAccessedAt
    }

    var identity: AssetBundleIdentity { AssetBundleIdentity(bundleID: bundleID, version: version) }

    func matches(_ manifest: AssetBundleManifest) -> Bool {
        guard identity == manifest.identity, files.count == manifest.files.count else { return false }
        let recorded = Dictionary(uniqueKeysWithValues: files.map { ($0.assetID, $0) })
        return manifest.files.allSatisfy { file in
            guard let entry = recorded[file.assetID] else { return false }
            return entry.byteSize == file.byteSize && entry.checksumSHA256 == file.checksumSHA256
        }
    }

    func installedBundle(in store: AssetBundleStore) -> InstalledAssetBundle {
        var urls: [String: URL] = [:]
        var roles: [String: AssetRole] = [:]
        for file in files {
            urls[file.assetID] = store.installedFileURL(identity, assetID: file.assetID)
            roles[file.assetID] = AssetRole(rawValue: file.role) ?? .texture
        }
        return InstalledAssetBundle(
            identity: identity,
            kind: AssetBundleKind(rawValue: kind) ?? .roomVariant,
            format: format,
            files: urls,
            roles: roles
        )
    }

    static func leastRecentlyUsedFirst(_ a: CacheEntryRecord, _ b: CacheEntryRecord) -> Bool {
        if a.lastAccessedAt != b.lastAccessedAt { return a.lastAccessedAt < b.lastAccessedAt }
        if a.bundleID != b.bundleID { return a.bundleID < b.bundleID }
        return a.version < b.version
    }
}
