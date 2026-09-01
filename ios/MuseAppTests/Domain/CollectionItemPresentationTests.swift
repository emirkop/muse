import XCTest
@testable import MuseApp

final class CollectionItemPresentationTests: XCTestCase {

    private func items(_ models: [String]) -> [CollectionItem] {
        models.enumerated().map { slot, modelID in
            CollectionItem(id: "item-\(slot)", slotIndex: slot, catalogModelID: modelID)
        }
    }

    private func resolver(_ catalog: FakePresentationAssetCatalog) -> CatalogCollectionItemPresentationResolver {
        CatalogCollectionItemPresentationResolver(catalog: catalog)
    }

    // MARK: - The three states

    func test_theThreeStatesAreResolvedAndKeptDistinct() async {
        let catalog = FakePresentationAssetCatalog()
        catalog.mapped = ["model-mapped": AssetBundleRef(id: "bundle_model_mapped", version: 2)]
        catalog.servedWithoutAsset = ["model-unmapped"]

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(["model-mapped", "model-unmapped", "model-absent"]), accessToken: "t"
        )

        guard case .available(let asset) = resolution.state(forModelID: "model-mapped") else {
            return XCTFail("expected available, got \(resolution.state(forModelID: "model-mapped"))")
        }
        XCTAssertEqual(asset.assetBundle, AssetBundleRef(id: "bundle_model_mapped", version: 2))
        XCTAssertEqual(asset.catalogModelID, "model-mapped")

        XCTAssertEqual(
            resolution.state(forModelID: "model-unmapped"),
            .notMapped(catalogModelID: "model-unmapped")
        )
        XCTAssertEqual(
            resolution.state(forModelID: "model-absent"),
            .unavailable(catalogModelID: "model-absent", reason: .modelUnknown)
        )
    }

    func test_anUnmappedModelYieldsNoAssetToFallBackOn() async {
        let catalog = FakePresentationAssetCatalog()
        catalog.servedWithoutAsset = ["model-unmapped"]

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(["model-unmapped"]), accessToken: "t"
        )
        let state = resolution.state(forModelID: "model-unmapped")

        XCTAssertNil(state.asset)
        XCTAssertFalse(state.hasResolvableAsset)
        XCTAssertEqual(state.catalogModelID, "model-unmapped")
    }

    func test_aFailedLookupIsTransientAndDistinctFromUnknown() async {
        let catalog = FakePresentationAssetCatalog()
        catalog.error = CollectionAPIError(statusCode: 503, code: nil, message: nil)

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(["model-a", "model-b"]), accessToken: "t"
        )

        for id in ["model-a", "model-b"] {
            XCTAssertEqual(
                resolution.state(forModelID: id),
                .unavailable(catalogModelID: id, reason: .lookupFailed)
            )
        }
    }

    func test_everyRequestedModelAppearsInTheResult() async {
        let catalog = FakePresentationAssetCatalog()
        catalog.mapped = ["a": AssetBundleRef(id: "bundle_a", version: 1)]

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(["a", "b", "c"]), accessToken: "t"
        )

        XCTAssertEqual(Set(resolution.statesByModelID.keys), ["a", "b", "c"])
        XCTAssertEqual(
            resolution.state(forModelID: "never-asked"),
            .unavailable(catalogModelID: "never-asked", reason: .lookupFailed)
        )
    }

    // MARK: - Batch behaviour

    func test_repeatedModelsAreResolvedOnce() async {
        let catalog = FakePresentationAssetCatalog()
        catalog.mapped = ["shared": AssetBundleRef(id: "bundle_shared", version: 1)]

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(["shared", "shared", "shared", "shared"]), accessToken: "t"
        )

        XCTAssertEqual(catalog.requests.count, 1, "one request for one distinct Model")
        XCTAssertEqual(catalog.requests.first, ["shared"], "the id must be requested once, not four times")
        XCTAssertEqual(resolution.statesByModelID.count, 1)
    }

    func test_aLargeRoomIsChunkedToTheServersBound() async {
        let catalog = FakePresentationAssetCatalog()
        let bound = CollectionPresentationAssetLookup.maxPerRequest
        let models = (0..<(bound * 2 + 5)).map { "model-\($0)" }
        for model in models {
            catalog.servedWithoutAsset.insert(model)
        }

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(models), accessToken: "t"
        )

        XCTAssertEqual(catalog.requests.count, 3, "\(models.count) ids should chunk into 3 requests")
        for request in catalog.requests {
            XCTAssertLessThanOrEqual(request.count, bound)
        }
        XCTAssertEqual(resolution.statesByModelID.count, models.count)
    }

    func test_aFailedChunkDoesNotDiscardTheRest() async {
        let catalog = FakePresentationAssetCatalog()
        let bound = CollectionPresentationAssetLookup.maxPerRequest
        let models = (0..<(bound + 3)).map { "model-\($0)" }
        for model in models {
            catalog.mapped[model] = AssetBundleRef(id: "bundle_\(model)", version: 1)
        }
        catalog.failRequestsAfter = 1

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(models), accessToken: "t"
        )

        let resolved = models.filter { resolution.state(forModelID: $0).hasResolvableAsset }
        XCTAssertEqual(resolved.count, bound, "the successful chunk must survive the failed one")
        let failed = models.filter {
            resolution.state(forModelID: $0) == .unavailable(catalogModelID: $0, reason: .lookupFailed)
        }
        XCTAssertEqual(failed.count, 3)
    }

    func test_anEmptyRoomResolvesToNothingWithoutAsking() async {
        let catalog = FakePresentationAssetCatalog()

        let resolution = await resolver(catalog).resolvePresentation(for: [], accessToken: "t")

        XCTAssertTrue(resolution.statesByModelID.isEmpty)
        XCTAssertTrue(catalog.requests.isEmpty, "an empty Room must issue no request")
    }

    // MARK: - Reporting, never dropping

    func test_itemsArePartitionedAndNothingIsDropped() async {
        let catalog = FakePresentationAssetCatalog()
        catalog.mapped = ["mapped": AssetBundleRef(id: "bundle_mapped", version: 1)]
        catalog.servedWithoutAsset = ["unmapped"]
        let room = items(["mapped", "unmapped", "absent", "mapped"])

        let resolution = await resolver(catalog).resolvePresentation(for: room, accessToken: "t")

        let resolvable = resolution.resolvable(among: room)
        let awaiting = resolution.awaitingAuthoredAsset(among: room)
        let unresolvable = resolution.unresolvable(among: room)

        XCTAssertEqual(resolvable.map(\.item.slotIndex), [0, 3])
        XCTAssertEqual(awaiting.map(\.slotIndex), [1])
        XCTAssertEqual(unresolvable.map(\.slotIndex), [2])
        XCTAssertEqual(
            resolvable.count + awaiting.count + unresolvable.count, room.count,
            "every item must land in exactly one partition — none may be dropped"
        )
        XCTAssertEqual(resolvable.map(\.asset.assetBundle.id), ["bundle_mapped", "bundle_mapped"])
    }

    // MARK: - The boundary

    func test_theResolverHoldsNoDeliveryServiceAndFetchesNoBytes() async {
        let catalog = FakePresentationAssetCatalog()
        catalog.mapped = ["m": AssetBundleRef(id: "bundle_m", version: 4)]

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(["m"]), accessToken: "t"
        )

        guard let asset = resolution.state(forModelID: "m").asset else {
            return XCTFail("expected an asset reference")
        }
        XCTAssertEqual(asset.assetBundle.id, "bundle_m")
        XCTAssertEqual(asset.assetBundle.version, 4)
        XCTAssertEqual(catalog.requests, [["m"]])
    }

    func test_aFixtureMappingIsLabelled() async {
        let catalog = FakePresentationAssetCatalog()
        catalog.mapped = ["m": AssetBundleRef(id: "dev_fixture_collection_model", version: 1)]
        catalog.fixtureModels = ["m"]

        let resolution = await resolver(catalog).resolvePresentation(
            for: items(["m"]), accessToken: "t"
        )

        XCTAssertEqual(resolution.state(forModelID: "m").asset?.isDevelopmentFixture, true)
    }

    func test_swappingThePlaceholderForAuthoredArtChangesOnlyWhatTheCatalogServes() async {
        let catalog = FakePresentationAssetCatalog()
        let room = items(["m"])
        catalog.mapped = ["m": AssetBundleRef(id: "dev_fixture_collection_model", version: 1)]
        catalog.fixtureModels = ["m"]

        let before = await resolver(catalog).resolvePresentation(for: room, accessToken: "t")
        XCTAssertEqual(before.state(forModelID: "m").asset?.assetBundle.id, "dev_fixture_collection_model")

        catalog.mapped = ["m": AssetBundleRef(id: "bundle_authored_watch", version: 3)]
        catalog.fixtureModels = []

        let after = await resolver(catalog).resolvePresentation(for: room, accessToken: "t")
        XCTAssertEqual(after.state(forModelID: "m").asset?.assetBundle, AssetBundleRef(id: "bundle_authored_watch", version: 3))
        XCTAssertEqual(after.state(forModelID: "m").asset?.isDevelopmentFixture, false)

        XCTAssertEqual(room, items(["m"]))
        XCTAssertEqual(after.resolvable(among: room).map(\.item), room)
    }
}

final class FakePresentationAssetCatalog: CollectionPresentationAssetReading, @unchecked Sendable {
    var mapped: [String: AssetBundleRef] = [:]
    var servedWithoutAsset: Set<String> = []
    var fixtureModels: Set<String> = []
    var failRequestsAfter: Int?
    var error: Error?

    private(set) var requests: [[String]] = []

    func fetchPresentationAssets(
        accessToken: String,
        catalogModelIDs: [String]
    ) async throws -> [CollectionPresentationAssetEntry] {
        requests.append(catalogModelIDs)
        if let error { throw error }
        if let failAfter = failRequestsAfter, requests.count > failAfter {
            throw CollectionAPIError(statusCode: 503, code: nil, message: nil)
        }

        var entries: [CollectionPresentationAssetEntry] = []
        for id in catalogModelIDs {
            if let bundle = mapped[id] {
                entries.append(CollectionPresentationAssetEntry(
                    catalogModelID: id,
                    asset: CollectionItemPresentationAsset(
                        catalogModelID: id,
                        assetBundle: bundle,
                        isDevelopmentFixture: fixtureModels.contains(id)
                    )
                ))
            } else if servedWithoutAsset.contains(id) {
                entries.append(CollectionPresentationAssetEntry(catalogModelID: id, asset: nil))
            }
        }
        return entries
    }
}
