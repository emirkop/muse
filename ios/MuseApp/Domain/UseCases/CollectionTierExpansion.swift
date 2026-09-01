import Foundation

public protocol CollectionTierRatcheting: Sendable {
    func ratchetTier(
        accessToken: String,
        collectionRoomID: String,
        tier: CollectionTier
    ) async throws -> CollectionRoom
}

public protocol CollectionTierGeometryInstalling: Sendable {
    func installTierGeometry(accessToken: String, bundle: AssetBundleRef) async -> Bool
}

public struct CollectionTierExpansion: Sendable {
    public struct Outcome: Equatable, Sendable {
        public let tier: CollectionTier
        public let expanded: Bool
        public let installedGeometry: [AssetBundleRef]
        public let failedGeometry: [AssetBundleRef]

        public var isFullyRendered: Bool { failedGeometry.isEmpty }
    }

    public enum Failure: Error, Equatable {
        case incoherentTable(CollectionTierTable.Rejection)
        case capacityExhausted(itemCount: Int, highestTier: CollectionTier)
        case negativeItemCount
        case ratchetRefused
    }

    private let ratchet: any CollectionTierRatcheting
    private let geometry: any CollectionTierGeometryInstalling

    public init(
        ratchet: any CollectionTierRatcheting,
        geometry: any CollectionTierGeometryInstalling
    ) {
        self.ratchet = ratchet
        self.geometry = geometry
    }

    public static func requiredTier(
        forItemCount itemCount: Int,
        table: CollectionTierTable
    ) -> Result<CollectionTier, Failure> {
        if let rejection = table.rejection {
            return .failure(.incoherentTable(rejection))
        }
        switch table.capacities.requiredTier(forItemCount: itemCount) {
        case .success(let tier):
            return .success(tier)
        case .failure(.negativeItemCount):
            return .failure(.negativeItemCount)
        case .failure(.exhausted):
            return .failure(.capacityExhausted(
                itemCount: itemCount,
                highestTier: table.highestTier ?? .base
            ))
        case .failure(.malformedTable(let rejection)):
            _ = rejection
            return .failure(.incoherentTable(.capacitiesNotStrictlyIncreasing))
        }
    }

    public func expand(
        room: CollectionRoom,
        toHold itemCount: Int,
        table: CollectionTierTable,
        accessToken: String
    ) async -> Result<Outcome, Failure> {
        let required: CollectionTier
        switch Self.requiredTier(forItemCount: itemCount, table: table) {
        case .success(let tier): required = tier
        case .failure(let failure): return .failure(failure)
        }

        guard required > room.currentTier else {
            return .success(Outcome(
                tier: room.currentTier, expanded: false,
                installedGeometry: [], failedGeometry: []
            ))
        }

        let updated: CollectionRoom
        do {
            updated = try await ratchet.ratchetTier(
                accessToken: accessToken,
                collectionRoomID: room.id,
                tier: required
            )
        } catch {
            return .failure(.ratchetRefused)
        }

        let newBundles = table.additionalGeometry(movingFrom: room.currentTier, to: updated.currentTier)

        var installed: [AssetBundleRef] = []
        var failed: [AssetBundleRef] = []
        for bundle in newBundles {
            if await geometry.installTierGeometry(accessToken: accessToken, bundle: bundle) {
                installed.append(bundle)
            } else {
                failed.append(bundle)
            }
        }

        return .success(Outcome(
            tier: updated.currentTier,
            expanded: updated.currentTier > room.currentTier,
            installedGeometry: installed,
            failedGeometry: failed
        ))
    }
}
