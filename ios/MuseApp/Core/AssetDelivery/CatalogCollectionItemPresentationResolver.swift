import Foundation

public struct CatalogCollectionItemPresentationResolver: CollectionItemPresentationResolving {
    private let catalog: any CollectionPresentationAssetReading

    public init(catalog: any CollectionPresentationAssetReading) {
        self.catalog = catalog
    }

    public func resolvePresentation(
        for items: [CollectionItem],
        accessToken: String
    ) async -> CollectionItemPresentationResolution {
        var requested: [String] = []
        var seen: Set<String> = []
        for item in items.sorted(by: { $0.slotIndex < $1.slotIndex }) {
            let id = item.catalogModelID
            guard !id.isEmpty, !seen.contains(id) else { continue }
            seen.insert(id)
            requested.append(id)
        }
        guard !requested.isEmpty else {
            return CollectionItemPresentationResolution(statesByModelID: [:])
        }

        var states: [String: CollectionItemPresentationState] = [:]
        for chunk in requested.chunked(into: CollectionPresentationAssetLookup.maxPerRequest) {
            let entries: [CollectionPresentationAssetEntry]
            do {
                entries = try await catalog.fetchPresentationAssets(
                    accessToken: accessToken, catalogModelIDs: chunk
                )
            } catch {
                for id in chunk {
                    states[id] = .unavailable(catalogModelID: id, reason: .lookupFailed)
                }
                continue
            }

            let served = Dictionary(
                entries.map { ($0.catalogModelID, $0) }, uniquingKeysWith: { first, _ in first }
            )
            for id in chunk {
                guard let entry = served[id] else {
                    states[id] = .unavailable(catalogModelID: id, reason: .modelUnknown)
                    continue
                }
                if let asset = entry.asset {
                    states[id] = .available(asset)
                } else {
                    states[id] = .notMapped(catalogModelID: id)
                }
            }
        }
        return CollectionItemPresentationResolution(statesByModelID: states)
    }
}

private extension Array {
    func chunked(into size: Int) -> [[Element]] {
        guard size > 0 else { return [self] }
        return stride(from: 0, to: count, by: size).map {
            Array(self[$0..<Swift.min($0 + size, count)])
        }
    }
}
