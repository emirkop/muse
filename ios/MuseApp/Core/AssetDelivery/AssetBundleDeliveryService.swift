import Foundation

public actor AssetBundleDeliveryService: AssetBundleProviding {
    private let manifests: any AssetBundleManifestFetching
    private let downloader: any AssetBundleFileDownloading
    private let cache: AssetBundleCache
    private let appAssetVersion: Int
    private let diagnostics: any ErrorReporting

    private var inFlight: [String: Task<Result<InstalledAssetBundle, AssetBundleDeliveryError>, Never>] = [:]

    public init(
        manifests: any AssetBundleManifestFetching,
        downloader: any AssetBundleFileDownloading = URLSessionAssetBundleDownloader(),
        cache: AssetBundleCache,
        appAssetVersion: Int = AssetBundleFormat.appAssetVersion,
        diagnostics: any ErrorReporting = NoErrorReporting()
    ) {
        self.manifests = manifests
        self.downloader = downloader
        self.cache = cache
        self.appAssetVersion = appAssetVersion
        self.diagnostics = diagnostics
    }

    public func bundle(
        accessToken: String,
        bundleID: String,
        progress: (@Sendable (AssetBundleDeliveryState) -> Void)?
    ) async -> Result<InstalledAssetBundle, AssetBundleDeliveryError> {
        if let existing = inFlight[bundleID] {
            return await existing.value
        }

        let task = Task { [manifests, downloader, cache, appAssetVersion, diagnostics] in
            await Self.resolve(
                accessToken: accessToken,
                bundleID: bundleID,
                manifests: manifests,
                downloader: downloader,
                cache: cache,
                appAssetVersion: appAssetVersion,
                diagnostics: diagnostics,
                progress: progress
            )
        }
        inFlight[bundleID] = task
        let result = await task.value
        inFlight[bundleID] = nil
        return result
    }

    public func installedVersions(ofBundle bundleID: String) -> [Int] {
        cache.store.installedVersions(ofBundle: bundleID)
    }

    // MARK: - Resolution

    private static func resolve(
        accessToken: String,
        bundleID: String,
        manifests: any AssetBundleManifestFetching,
        downloader: any AssetBundleFileDownloading,
        cache: AssetBundleCache,
        appAssetVersion: Int,
        diagnostics: any ErrorReporting,
        progress: (@Sendable (AssetBundleDeliveryState) -> Void)?
    ) async -> Result<InstalledAssetBundle, AssetBundleDeliveryError> {
        let outcome = await resolveWithoutReporting(
            accessToken: accessToken, bundleID: bundleID, manifests: manifests,
            downloader: downloader, cache: cache, appAssetVersion: appAssetVersion,
            progress: progress
        )
        if case .failure(let error) = outcome {
            diagnostics.report(ErrorReport(
                domain: .assetDelivery,
                reason: ErrorReport.Reason(deliveryError: error),
                bundle: bundleID
            ))
        }
        return outcome
    }

    private static func resolveWithoutReporting(
        accessToken: String,
        bundleID: String,
        manifests: any AssetBundleManifestFetching,
        downloader: any AssetBundleFileDownloading,
        cache: AssetBundleCache,
        appAssetVersion: Int,
        progress: (@Sendable (AssetBundleDeliveryState) -> Void)?
    ) async -> Result<InstalledAssetBundle, AssetBundleDeliveryError> {
        let manifest: AssetBundleManifest
        do {
            manifest = try await manifests.manifest(
                accessToken: accessToken, bundleID: bundleID, appAssetVersion: appAssetVersion
            )
        } catch let error as PhotoAPIError {
            let failure: AssetBundleDeliveryError = switch error.statusCode {
            case 404: .notPublished
            case 503: .deliveryUnconfigured
            default: .manifestUnreachable
            }
            progress?(.failed(failure))
            return .failure(failure)
        } catch {
            guard NetworkResilience.permitsCachedRead(NetworkResilience.classify(error)) else {
                progress?(.failed(.manifestUnreachable))
                return .failure(.manifestUnreachable)
            }
            if let installed = await cache.offlineEntry(bundleID: bundleID) {
                progress?(.installed(installed))
                return .success(installed)
            }
            progress?(.failed(.offline))
            return .failure(.offline)
        }

        guard manifest.geometryFile != nil else {
            progress?(.failed(.malformedBundle))
            return .failure(.malformedBundle)
        }

        if let installed = await cache.hit(manifest) {
            progress?(.installed(installed))
            return .success(installed)
        }

        let store = cache.store
        do {
            try store.prepare(for: manifest.identity)
        } catch {
            progress?(.failed(.storageFailed))
            return .failure(.storageFailed)
        }

        let total = max(manifest.totalByteSize, 1)
        let reporter = ByteProgress(
            baseline: store.installedByteCount(manifest),
            total: total,
            progress: progress
        )
        reporter.emit(geometryReady: false)

        var geometryInstalled = false
        for file in manifest.files {
            let destination = store.installedFileURL(manifest.identity, assetID: file.assetID)
            if FileManager.default.fileExists(atPath: destination.path) {
                if (try? URLSessionAssetBundleDownloader.sha256Hex(of: destination)) == file.checksumSHA256 {
                    if file.role == .geometry { geometryInstalled = true }
                    continue
                }
                try? FileManager.default.removeItem(at: destination)
            }

            let geometryReadyNow = geometryInstalled
            do {
                try await downloader.download(
                    file: file,
                    to: destination,
                    partial: store.partialFileURL(manifest.identity, assetID: file.assetID),
                    onBytes: { bytes in reporter.report(assetID: file.assetID, bytes: bytes, geometryReady: geometryReadyNow) }
                )
            } catch AssetBundleDownloadError.integrity {
                progress?(.failed(.integrityCheckFailed(assetID: file.assetID)))
                return .failure(.integrityCheckFailed(assetID: file.assetID))
            } catch AssetBundleDownloadError.fileSystem {
                progress?(.failed(.storageFailed))
                return .failure(.storageFailed)
            } catch is CancellationError {
                return .failure(.downloadFailed)
            } catch {
                progress?(.failed(.downloadFailed))
                return .failure(.downloadFailed)
            }

            reporter.complete(assetID: file.assetID, byteSize: file.byteSize)
            if file.role == .geometry {
                geometryInstalled = true
            }
            reporter.emit(geometryReady: geometryInstalled)
        }

        do {
            let committed = try await cache.commit(manifest)
            progress?(.installed(committed.bundle))
            return .success(committed.bundle)
        } catch {
            progress?(.failed(.storageFailed))
            return .failure(.storageFailed)
        }
    }
}

final class ByteProgress: @unchecked Sendable {
    private let lock = NSLock()
    private let total: Int64
    private let progress: (@Sendable (AssetBundleDeliveryState) -> Void)?
    private var completedBytes: Int64
    private var inFlight: [String: Int64] = [:]
    private var lastGeometryReady = false

    init(baseline: Int64, total: Int64, progress: (@Sendable (AssetBundleDeliveryState) -> Void)?) {
        self.completedBytes = baseline
        self.total = max(total, 1)
        self.progress = progress
    }

    func report(assetID: String, bytes: Int64, geometryReady: Bool) {
        lock.lock()
        inFlight[assetID] = bytes
        lastGeometryReady = lastGeometryReady || geometryReady
        let fraction = self.fractionLocked()
        let ready = lastGeometryReady
        lock.unlock()
        publish(fraction: fraction, geometryReady: ready)
    }

    func complete(assetID: String, byteSize: Int64) {
        lock.lock()
        inFlight[assetID] = nil
        completedBytes += byteSize
        lock.unlock()
    }

    func emit(geometryReady: Bool) {
        lock.lock()
        lastGeometryReady = lastGeometryReady || geometryReady
        let fraction = fractionLocked()
        let ready = lastGeometryReady
        lock.unlock()
        publish(fraction: fraction, geometryReady: ready)
    }

    private func fractionLocked() -> Double {
        let known = completedBytes + inFlight.values.reduce(0, +)
        return min(1, max(0, Double(known) / Double(total)))
    }

    private func publish(fraction: Double, geometryReady: Bool) {
        guard let progress else { return }
        progress(geometryReady ? .geometryReady(fractionComplete: fraction) : .downloading(fractionComplete: fraction))
    }
}
