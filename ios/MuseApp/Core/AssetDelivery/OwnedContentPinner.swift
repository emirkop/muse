import Foundation

public actor OwnedContentPinner {
    private let museums: any MuseumServicing
    private let catalog: any CatalogServicing
    private let retention: any AssetBundleRetaining
    private var held: [AssetBundleIdentity: AssetBundleLease] = [:]

    public init(museums: any MuseumServicing, catalog: any CatalogServicing, retention: any AssetBundleRetaining) {
        self.museums = museums
        self.catalog = catalog
        self.retention = retention
    }

    public struct Refresh: Equatable, Sendable {
        public var pinned: Set<AssetBundleIdentity>
        public var newlyPinned: Set<AssetBundleIdentity>
        public var released: Set<AssetBundleIdentity>
        public var unchangedBecauseUnreadable: Bool
    }

    public func refresh(accessToken: String) async -> Refresh {
        let wanted: Set<AssetBundleIdentity>
        do {
            wanted = try await ownedIdentities(accessToken: accessToken)
        } catch OwnedContentUnreadable.noMuseum {
            wanted = []
        } catch {
            return Refresh(pinned: Set(held.keys), newlyPinned: [], released: [], unchangedBecauseUnreadable: true)
        }

        var newlyPinned: Set<AssetBundleIdentity> = []
        var released: Set<AssetBundleIdentity> = []
        for identity in wanted where held[identity] == nil {
            held[identity] = retention.retain(identity)
            newlyPinned.insert(identity)
        }
        for (identity, lease) in held where !wanted.contains(identity) {
            retention.release(lease)
            held[identity] = nil
            released.insert(identity)
        }
        return Refresh(pinned: wanted, newlyPinned: newlyPinned, released: released, unchangedBecauseUnreadable: false)
    }

    public func clear() {
        for lease in held.values {
            retention.release(lease)
        }
        held = [:]
    }

    public var pinnedIdentities: Set<AssetBundleIdentity> { Set(held.keys) }

    private enum OwnedContentUnreadable: Error { case noMuseum }

    private func ownedIdentities(accessToken: String) async throws -> Set<AssetBundleIdentity> {
        let museum: Museum
        do {
            museum = try await museums.fetchMuseum(accessToken: accessToken)
        } catch IdentityAPIClientError.server(let statusCode, _) where statusCode == 404 {
            throw OwnedContentUnreadable.noMuseum
        }
        let rooms = try await museums.listRooms(accessToken: accessToken)
        let styles = try await catalog.fetchStyles(accessToken: accessToken)
        let variants = try await catalog.fetchVariants(accessToken: accessToken, styleID: museum.styleID)

        var wanted: Set<AssetBundleIdentity> = []
        if let style = styles.first(where: { $0.id == museum.styleID }) {
            wanted.insert(AssetBundleIdentity(bundleID: style.assetBundle.id, version: style.assetBundle.version))
        }
        let variantBundles = Dictionary(uniqueKeysWithValues: variants.map { ($0.id, $0.assetBundle) })
        for room in rooms {
            if let bundle = variantBundles[room.variantID] {
                wanted.insert(AssetBundleIdentity(bundleID: bundle.id, version: bundle.version))
            }
        }
        return wanted
    }
}
