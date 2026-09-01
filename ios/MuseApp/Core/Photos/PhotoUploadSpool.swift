import Foundation

public struct PhotoUploadSpool: Sendable {
    public let directory: URL

    public static let shared = PhotoUploadSpool(
        directory: FileManager.default
            .urls(for: .cachesDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("PhotoUploadSpool", isDirectory: true)
    )

    public init(directory: URL) {
        self.directory = directory
    }

    public func prepare() throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        var dir = directory
        try dir.setResourceValues(values)
    }

    public func fileURL(forPickedPhotoID id: String) -> URL {
        directory.appendingPathComponent("\(id).jpg", isDirectory: false)
    }

    public func remove(_ url: URL) {
        try? FileManager.default.removeItem(at: url)
    }

    public func purgeAll() {
        guard let contents = try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil) else {
            return
        }
        for url in contents {
            try? FileManager.default.removeItem(at: url)
        }
    }

    public func contents() -> [URL] {
        (try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)) ?? []
    }
}
