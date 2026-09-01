import CryptoKit
import Foundation

public protocol AssetBundleFileDownloading: Sendable {
    func download(
        file: AssetBundleFile,
        to destination: URL,
        partial: URL,
        onBytes: (@Sendable (Int64) -> Void)?
    ) async throws
}

public enum AssetBundleDownloadError: Error, Equatable, Sendable {
    case transport
    case httpStatus(Int)
    case integrity
    case fileSystem
}

public struct URLSessionAssetBundleDownloader: AssetBundleFileDownloading {
    private let session: URLSession

    public static func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.default
        configuration.urlCache = nil
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 600
        configuration.httpMaximumConnectionsPerHost = 3
        configuration.waitsForConnectivity = false
        return URLSession(configuration: configuration)
    }

    public init(session: URLSession? = nil) {
        self.session = session ?? Self.makeSession()
    }

    public func download(
        file: AssetBundleFile,
        to destination: URL,
        partial: URL,
        onBytes: (@Sendable (Int64) -> Void)?
    ) async throws {
        let existing = Self.byteCount(at: partial)

        if existing == file.byteSize {
            onBytes?(existing)
            try Self.verifyAndInstall(partial: partial, destination: destination, file: file)
            return
        }

        var offset = existing
        if existing > file.byteSize {
            try? FileManager.default.removeItem(at: partial)
            offset = 0
        }

        do {
            try await fetch(file: file, from: offset, partial: partial, onBytes: onBytes)
        } catch ResumeRefused.byServer where offset > 0 {
            try? FileManager.default.removeItem(at: partial)
            try await fetch(file: file, from: 0, partial: partial, onBytes: onBytes)
        }
        try Self.verifyAndInstall(partial: partial, destination: destination, file: file)
    }

    // MARK: - Transfer

    private enum ResumeRefused: Error { case byServer }

    private func fetch(
        file: AssetBundleFile,
        from offset: Int64,
        partial: URL,
        onBytes: (@Sendable (Int64) -> Void)?
    ) async throws {
        var request = URLRequest(url: file.url)
        request.httpMethod = "GET"
        if offset > 0 {
            request.setValue("bytes=\(offset)-", forHTTPHeaderField: "Range")
        }

        let sink = try DownloadSink(
            partial: partial,
            truncating: offset == 0,
            startingAt: offset,
            expecting: file,
            onBytes: onBytes
        )
        let task = session.dataTask(with: request)
        task.delegate = sink

        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                sink.setCompletion { result in
                    continuation.resume(with: result)
                }
                task.resume()
            }
        } onCancel: {
            task.cancel()
        }
    }

    // MARK: - Verification and install

    static func verifyAndInstall(partial: URL, destination: URL, file: AssetBundleFile) throws {
        guard byteCount(at: partial) == file.byteSize else {
            try? FileManager.default.removeItem(at: partial)
            throw AssetBundleDownloadError.integrity
        }
        guard let digest = try? sha256Hex(of: partial) else {
            throw AssetBundleDownloadError.fileSystem
        }
        guard digest == file.checksumSHA256 else {
            try? FileManager.default.removeItem(at: partial)
            throw AssetBundleDownloadError.integrity
        }

        let manager = FileManager.default
        do {
            try manager.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
            if manager.fileExists(atPath: destination.path) {
                try manager.removeItem(at: destination)
            }
            try manager.moveItem(at: partial, to: destination)
        } catch {
            throw AssetBundleDownloadError.fileSystem
        }
    }

    static func sha256Hex(of url: URL) throws -> String {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        var hasher = SHA256()
        while let chunk = try handle.read(upToCount: 256 * 1024), !chunk.isEmpty {
            hasher.update(data: chunk)
        }
        return hasher.finalize().map { String(format: "%02x", $0) }.joined()
    }

    static func byteCount(at url: URL) -> Int64 {
        guard let attributes = try? FileManager.default.attributesOfItem(atPath: url.path),
              let size = attributes[.size] as? NSNumber else {
            return 0
        }
        return size.int64Value
    }

    static func contentRange(_ header: String, matchesStart start: Int64, total: Int64) -> Bool {
        let trimmed = header.trimmingCharacters(in: .whitespaces)
        guard trimmed.hasPrefix("bytes ") else { return false }
        let spec = trimmed.dropFirst("bytes ".count)
        let parts = spec.split(separator: "/", maxSplits: 1)
        guard parts.count == 2, let reportedTotal = Int64(parts[1]), reportedTotal == total else { return false }
        let range = parts[0].split(separator: "-", maxSplits: 1)
        guard range.count == 2,
              let reportedStart = Int64(range[0]), reportedStart == start,
              let reportedEnd = Int64(range[1]), reportedEnd == total - 1 else { return false }
        return true
    }

    // MARK: - The chunk sink

    private final class DownloadSink: NSObject, URLSessionDataDelegate, @unchecked Sendable {
        private let lock = NSLock()
        private let handle: FileHandle
        private let expecting: AssetBundleFile
        private let startingAt: Int64
        private let onBytes: (@Sendable (Int64) -> Void)?

        private var written: Int64
        private var failure: Error?
        private var finished = false
        private var completion: ((Result<Void, Error>) -> Void)?
        private var pending: Result<Void, Error>?

        init(partial: URL, truncating: Bool, startingAt: Int64, expecting: AssetBundleFile, onBytes: (@Sendable (Int64) -> Void)?) throws {
            let manager = FileManager.default
            try manager.createDirectory(at: partial.deletingLastPathComponent(), withIntermediateDirectories: true)
            if truncating || !manager.fileExists(atPath: partial.path) {
                manager.createFile(atPath: partial.path, contents: nil)
            }
            let handle = try FileHandle(forWritingTo: partial)
            if truncating {
                try handle.truncate(atOffset: 0)
            } else {
                try handle.seekToEnd()
            }
            self.handle = handle
            self.expecting = expecting
            self.startingAt = startingAt
            self.onBytes = onBytes
            self.written = startingAt
            super.init()
        }

        func urlSession(
            _ session: URLSession,
            dataTask: URLSessionDataTask,
            didReceive response: URLResponse,
            completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
        ) {
            guard let http = response as? HTTPURLResponse else {
                finish(.failure(AssetBundleDownloadError.transport))
                completionHandler(.cancel)
                return
            }
            if startingAt > 0 {
                guard http.statusCode == 206,
                      let contentRange = http.value(forHTTPHeaderField: "Content-Range"),
                      URLSessionAssetBundleDownloader.contentRange(contentRange, matchesStart: startingAt, total: expecting.byteSize) else {
                    finish(.failure(ResumeRefused.byServer))
                    completionHandler(.cancel)
                    return
                }
            } else if http.statusCode != 200 {
                finish(.failure(AssetBundleDownloadError.httpStatus(http.statusCode)))
                completionHandler(.cancel)
                return
            }
            completionHandler(.allow)
        }

        func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
            lock.lock()
            let alreadyFailed = failure != nil || finished
            if !alreadyFailed {
                do {
                    try handle.write(contentsOf: data)
                    written += Int64(data.count)
                } catch {
                    failure = AssetBundleDownloadError.fileSystem
                }
            }
            let total = written
            let failed = failure
            lock.unlock()

            if failed == nil, !alreadyFailed {
                onBytes?(total)
            }
        }

        func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
            lock.lock()
            let recorded = failure
            lock.unlock()
            try? handle.close()

            if let recorded {
                finish(.failure(recorded))
                return
            }
            if let error {
                if (error as NSError).code == NSURLErrorCancelled {
                    finish(.failure(CancellationError()))
                } else {
                    finish(.failure(AssetBundleDownloadError.transport))
                }
                return
            }
            finish(.success(()))
        }

        func setCompletion(_ completion: @escaping (Result<Void, Error>) -> Void) {
            lock.lock()
            if let pending {
                self.pending = nil
                lock.unlock()
                completion(pending)
                return
            }
            self.completion = completion
            lock.unlock()
        }

        private func finish(_ result: Result<Void, Error>) {
            lock.lock()
            if finished {
                lock.unlock()
                return
            }
            finished = true
            guard let callback = completion else {
                pending = result
                lock.unlock()
                return
            }
            completion = nil
            lock.unlock()
            callback(result)
        }
    }
}
