import Foundation

public actor DeliveredVariantLayoutProvider: RoomVariantSlotTableProviding, RoomDesignProviding {
    private let bundles: any AssetBundleProviding
    private let variants: any RoomVariantCatalogLookup
    private let accessToken: @Sendable () async -> String?

    private var decoded: [String: (identity: AssetBundleIdentity, layout: RoomVariantLayoutFile.Decoded)] = [:]

    public init(
        bundles: any AssetBundleProviding,
        variants: any RoomVariantCatalogLookup,
        accessToken: @escaping @Sendable () async -> String?
    ) {
        self.bundles = bundles
        self.variants = variants
        self.accessToken = accessToken
    }

    public func slotTable(forVariantID variantID: String) async -> RoomVariantSlotTable? {
        await resolve(forVariantID: variantID)?.layout.table
    }

    public func layout(forVariantID variantID: String) async -> RoomVariantLayoutFile.Decoded? {
        await resolve(forVariantID: variantID)?.layout
    }

    public struct Resolved: Sendable {
        public let installed: InstalledAssetBundle
        public let layout: RoomVariantLayoutFile.Decoded
    }

    public func resolve(forVariantID variantID: String) async -> Resolved? {
        if case .success(let resolved) = await resolveDetailed(forVariantID: variantID, progress: { _ in }) {
            return resolved
        }
        return nil
    }

    // MARK: RoomDesignProviding

    public func design(
        forVariantID variantID: String,
        progress: @escaping @Sendable (RoomDesignLoadState) -> Void
    ) async -> RoomDesignResolution {
        switch await resolveDetailed(forVariantID: variantID, progress: progress) {
        case .failure(let reason):
            return .unavailable(reason)
        case .success(let resolved):
            guard let geometryURL = resolved.installed.geometryURL else {
                return .unavailable(.malformedBundle)
            }
            return .available(RoomDesign(
                slotTable: resolved.layout.table,
                geometry: .variantBundle(RoomVariantGeometry(
                    variantID: variantID,
                    identity: resolved.installed.identity,
                    format: resolved.installed.format,
                    fileURL: geometryURL,
                    entry: resolved.layout.entry
                ))
            ))
        }
    }

    private func resolveDetailed(
        forVariantID variantID: String,
        progress: @escaping @Sendable (RoomDesignLoadState) -> Void
    ) async -> Result<Resolved, RoomDesignUnavailableReason> {
        progress(.checking)
        guard let token = await accessToken() else { return .failure(.network) }

        let variant: RoomVariant?
        do {
            variant = try await variants.variant(accessToken: token, variantID: variantID)
        } catch {
            return .failure(.network)
        }
        guard let variant else { return .failure(.variantUnknown) }

        let result = await bundles.bundle(
            accessToken: token,
            bundleID: variant.assetBundle.id,
            progress: { state in
                switch state {
                case .notStarted: progress(.checking)
                case .downloading(let fraction): progress(.downloading(fractionComplete: fraction))
                case .geometryReady(let fraction): progress(.geometryReady(fractionComplete: fraction))
                case .installed: progress(.ready)
                case .failed: break
                }
            }
        )
        let installed: InstalledAssetBundle
        switch result {
        case .success(let bundle):
            installed = bundle
        case .failure(let error):
            return .failure(Self.reason(for: error))
        }

        if let cached = decoded[variantID], cached.identity == installed.identity {
            return .success(Resolved(installed: installed, layout: cached.layout))
        }
        guard let layoutURL = installed.layoutURL,
              let layout = try? RoomVariantLayoutFile.decode(contentsOf: layoutURL) else {
            return .failure(.malformedBundle)
        }
        guard layout.table.variantID == variantID else { return .failure(.layoutMismatch) }

        decoded[variantID] = (installed.identity, layout)
        return .success(Resolved(installed: installed, layout: layout))
    }

    private static func reason(for error: AssetBundleDeliveryError) -> RoomDesignUnavailableReason {
        switch error {
        case .notPublished: return .notPublished
        case .deliveryUnconfigured: return .deliveryUnconfigured
        case .offline: return .offline
        case .manifestUnreachable, .downloadFailed: return .network
        case .integrityCheckFailed: return .corruptDownload
        case .storageFailed: return .storage
        case .malformedBundle: return .malformedBundle
        }
    }
}

public actor DeliveredPreviewAssetProvider: PreviewAssetProviding {
    private let bundles: any AssetBundleProviding
    private let accessToken: @Sendable () async -> String?
    private var latest: [String: AssetBundleDeliveryState] = [:]

    public init(
        bundles: any AssetBundleProviding,
        accessToken: @escaping @Sendable () async -> String?
    ) {
        self.bundles = bundles
        self.accessToken = accessToken
    }

    public func availability(for subject: PreviewSubject) async -> PreviewAssetAvailability {
        guard let token = await accessToken() else { return .unavailable }

        let bundleID = subject.assetBundle.id
        let result = await bundles.bundle(accessToken: token, bundleID: bundleID, progress: { [weak self] state in
            Task { await self?.record(state, for: bundleID) }
        })
        switch result {
        case .success:
            return .ready
        case .failure(let error):
            switch latest[bundleID] {
            case .downloading(let fraction): return .downloading(fractionComplete: fraction)
            case .geometryReady: return .geometryReady
            default:
                switch error {
                case .offline, .manifestUnreachable, .downloadFailed:
                    return .unreachable
                case .notPublished, .deliveryUnconfigured, .integrityCheckFailed,
                     .storageFailed, .malformedBundle:
                    return .unavailable
                }
            }
        }
    }

    private func record(_ state: AssetBundleDeliveryState, for bundleID: String) {
        latest[bundleID] = state
    }
}

public struct DeliveredCollectionTierGeometry: CollectionTierGeometryInstalling {
    private let bundles: any AssetBundleProviding

    public init(bundles: any AssetBundleProviding) {
        self.bundles = bundles
    }

    public func installTierGeometry(accessToken: String, bundle: AssetBundleRef) async -> Bool {
        switch await bundles.bundle(accessToken: accessToken, bundleID: bundle.id, progress: nil) {
        case .success:
            return true
        case .failure:
            return false
        }
    }
}

public actor DeliveredCollectionDesignTableProvider: CollectionDesignTableProviding {
    private let bundles: any AssetBundleProviding
    private let catalog: any CollectionCatalogServicing

    private var decoded: [String: (identity: AssetBundleIdentity, table: CollectionTierTable)] = [:]

    public init(bundles: any AssetBundleProviding, catalog: any CollectionCatalogServicing) {
        self.bundles = bundles
        self.catalog = catalog
    }

    public func tierTable(
        accessToken: String,
        categoryID: String,
        designID: String
    ) async -> CollectionTierTable? {
        let designs: [CollectionDesign]
        do {
            designs = try await catalog.fetchCollectionDesigns(accessToken: accessToken, categoryID: categoryID)
        } catch {
            return nil
        }
        guard let design = designs.first(where: { $0.id == designID }) else { return nil }

        guard case .success(let installed) = await bundles.bundle(
            accessToken: accessToken,
            bundleID: design.assetBundle.id,
            progress: nil
        ) else {
            return nil
        }

        if let cached = decoded[designID], cached.identity == installed.identity {
            return cached.table
        }
        guard let layoutURL = installed.layoutURL,
              let table = try? CollectionDesignLayoutFile.decode(contentsOf: layoutURL) else {
            return nil
        }
        guard table.designID == designID else { return nil }

        decoded[designID] = (installed.identity, table)
        return table
    }
}
