import Foundation

public struct AssetBundleStore: Sendable {
    private let root: URL

    public static func defaultRoot() -> URL {
        let caches = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first
            ?? URL(fileURLWithPath: NSTemporaryDirectory())
        return caches.appendingPathComponent("MuseAssetBundles", isDirectory: true)
    }

    public init(root: URL = AssetBundleStore.defaultRoot()) {
        self.root = root
    }

    // MARK: - Paths

    private func bundleDirectory(_ bundleID: String) -> URL {
        root.appendingPathComponent(sanitised(bundleID), isDirectory: true)
    }

    func versionDirectory(_ identity: AssetBundleIdentity) -> URL {
        bundleDirectory(identity.bundleID).appendingPathComponent("v\(identity.version)", isDirectory: true)
    }

    func installedFileURL(_ identity: AssetBundleIdentity, assetID: String) -> URL {
        versionDirectory(identity).appendingPathComponent(sanitised(assetID), isDirectory: false)
    }

    func entryRecordURL(_ identity: AssetBundleIdentity) -> URL {
        bundleDirectory(identity.bundleID).appendingPathComponent("v\(identity.version).entry.json", isDirectory: false)
    }

    func bundleIDsOnDisk() -> [String] {
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: root.path) else { return [] }
        return names.filter { !$0.hasPrefix(".") }.sorted()
    }

    func recordedVersions(ofBundle bundleID: String) -> [Int] {
        let directory = bundleDirectory(bundleID)
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: directory.path) else { return [] }
        return names.compactMap { name in
            guard name.hasPrefix("v"), name.hasSuffix(".entry.json") else { return nil }
            return Int(name.dropFirst().dropLast(".entry.json".count))
        }.sorted()
    }

    func removeVersion(_ identity: AssetBundleIdentity) {
        removeIncompleteInstall(identity)
        let manager = FileManager.default
        let staging = root.appendingPathComponent(".staging", isDirectory: true)
        let prefix = "\(sanitised(identity.bundleID))-v\(identity.version)-"
        if let names = try? manager.contentsOfDirectory(atPath: staging.path) {
            for name in names where name.hasPrefix(prefix) {
                try? manager.removeItem(at: staging.appendingPathComponent(name))
            }
        }
    }

    func removeIncompleteInstall(_ identity: AssetBundleIdentity) {
        let manager = FileManager.default
        try? manager.removeItem(at: entryRecordURL(identity))
        try? manager.removeItem(at: versionDirectory(identity))
        let bundle = bundleDirectory(identity.bundleID)
        if let remaining = try? manager.contentsOfDirectory(atPath: bundle.path), remaining.isEmpty {
            try? manager.removeItem(at: bundle)
        }
    }

    func partialFileURL(_ identity: AssetBundleIdentity, assetID: String) -> URL {
        root
            .appendingPathComponent(".staging", isDirectory: true)
            .appendingPathComponent("\(sanitised(identity.bundleID))-v\(identity.version)-\(sanitised(assetID)).part")
    }

    private func sanitised(_ component: String) -> String {
        let allowed = Set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")
        let mapped = String(component.map { allowed.contains($0) ? $0 : "_" })
        return mapped.isEmpty ? "_" : mapped
    }

    // MARK: - Queries

    public func installedVersions(ofBundle bundleID: String) -> [Int] {
        let directory = bundleDirectory(bundleID)
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: directory.path) else { return [] }
        return names
            .compactMap { $0.hasPrefix("v") ? Int($0.dropFirst()) : nil }
            .sorted(by: >)
    }

    public func installedByteCount(_ manifest: AssetBundleManifest) -> Int64 {
        manifest.files.reduce(0) { total, file in
            let url = installedFileURL(manifest.identity, assetID: file.assetID)
            return FileManager.default.fileExists(atPath: url.path) ? total + file.byteSize : total
        }
    }

    // MARK: - Mutations

    public func prepare(for identity: AssetBundleIdentity) throws {
        var directory = versionDirectory(identity)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try? directory.setResourceValues(values)
    }

    @discardableResult
    public func removeVersions(ofBundle bundleID: String, keeping keep: Int) -> [Int] {
        var removed: [Int] = []
        let superseded = Set(installedVersions(ofBundle: bundleID) + recordedVersions(ofBundle: bundleID))
        for version in superseded.sorted() where version != keep {
            let identity = AssetBundleIdentity(bundleID: bundleID, version: version)
            let existed = FileManager.default.fileExists(atPath: versionDirectory(identity).path)
            removeVersion(identity)
            if existed {
                removed.append(version)
            }
        }
        removeStalePartials(ofBundle: bundleID, keeping: keep)
        return removed
    }

    private func removeStalePartials(ofBundle bundleID: String, keeping keep: Int) {
        let staging = root.appendingPathComponent(".staging", isDirectory: true)
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: staging.path) else { return }
        let prefix = "\(sanitised(bundleID))-v"
        let current = "\(prefix)\(keep)-"
        for name in names where name.hasPrefix(prefix) && !name.hasPrefix(current) {
            try? FileManager.default.removeItem(at: staging.appendingPathComponent(name))
        }
    }

    public func removeAll() {
        try? FileManager.default.removeItem(at: root)
    }
}
