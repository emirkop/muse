import XCTest
@testable import MuseApp

@MainActor
final class CollectionModelSearchViewModelTests: XCTestCase {

    private func makeViewModel(
        catalog: FakeCollectionCatalog,
        categoryID: String = "category_watches",
        pageSize: Int = 25
    ) -> CollectionModelSearchViewModel {
        CollectionModelSearchViewModel(
            categoryID: categoryID,
            categoryDisplayName: "Watches",
            catalog: catalog,
            accessToken: "token",
            pageSize: pageSize
        )
    }

    // MARK: - Scope

    func testSearchIsAlwaysScopedToTheRoomsCategory() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog, categoryID: "category_hot_wheels")

        await viewModel.search()
        await viewModel.updateQuery("racer")

        XCTAssertEqual(catalog.searchCalls.count, 2)
        XCTAssertTrue(catalog.searchCalls.allSatisfy { $0.categoryID == "category_hot_wheels" })

        guard case .results(let models, _) = viewModel.state else {
            return XCTFail("expected results, got \(viewModel.state)")
        }
        XCTAssertEqual(models.map(\.id), ["dev-fixture:model-racer"])
    }

    func testAnEmptyQueryBrowsesTheCategory() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.search()

        guard case .results(let models, let canLoadMore) = viewModel.state else {
            return XCTFail("expected results, got \(viewModel.state)")
        }
        XCTAssertEqual(models.count, 3, "all three Watches fixtures")
        XCTAssertFalse(canLoadMore)
        XCTAssertEqual(catalog.searchCalls.first?.query, "")
    }

    func testResultsKeepTheServersOrder() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.search()

        guard case .results(let models, _) = viewModel.state else {
            return XCTFail("expected results")
        }
        XCTAssertEqual(
            models.map(\.displayName),
            [
                "Devco Chrono One (development fixture)",
                "Devco Chrono Two (development fixture)",
                "Testmark Diver (development fixture)"
            ]
        )
    }

    // MARK: - The distinction `02` requires

    func testNoResultsAndServiceFailureAreDifferentStates() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.updateQuery("nothingmatchesthis")
        guard case .noResults(let query) = viewModel.state else {
            return XCTFail("an empty result set must be noResults, got \(viewModel.state)")
        }
        XCTAssertEqual(query, "nothingmatchesthis")

        catalog.searchError = URLError(.notConnectedToInternet)
        await viewModel.updateQuery("chrono")
        guard case .failed = viewModel.state else {
            XCTFail("a transport failure must be failed, never noResults — got \(viewModel.state)")
            return
        }
    }

    func testRetryAfterFailureRecovers() async {
        let catalog = FakeCollectionCatalog()
        catalog.searchError = URLError(.timedOut)
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.search()
        guard case .failed = viewModel.state else { return XCTFail("expected failed") }

        catalog.searchError = nil
        await viewModel.search()
        guard case .results(let models, _) = viewModel.state else {
            return XCTFail("retry must recover, got \(viewModel.state)")
        }
        XCTAssertEqual(models.count, 3)
    }

    func testAServerRejectionReadsDifferentlyFromAConnectionFailure() async {
        let catalog = FakeCollectionCatalog()
        catalog.searchError = CollectionAPIError(
            statusCode: 400, code: "unknown category_id", message: nil
        )
        let rejected = makeViewModel(catalog: catalog)
        await rejected.search()

        catalog.searchError = URLError(.notConnectedToInternet)
        let offline = makeViewModel(catalog: catalog)
        await offline.search()

        guard case .failed(let rejectedMessage) = rejected.state,
              case .failed(let offlineMessage) = offline.state else {
            return XCTFail("both must be failed: \(rejected.state) / \(offline.state)")
        }
        XCTAssertNotEqual(rejectedMessage, offlineMessage)
        XCTAssertTrue(rejectedMessage.contains("Reopen the room"))
        XCTAssertTrue(offlineMessage.lowercased().contains("offline"), offlineMessage)
        XCTAssertFalse(rejectedMessage.lowercased().contains("offline"),
                       "a server rejection must never be explained as being offline")
    }

    // MARK: - Live typing

    func testAStaleResponseIsDroppedRatherThanShown() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        let released = expectation(description: "first search released")
        catalog.gate = {
            catalog.gate = nil
            await withCheckedContinuation { continuation in
                Task { @MainActor in
                    released.fulfill()
                    continuation.resume()
                }
            }
        }

        let slow = Task { await viewModel.updateQuery("diver") }
        await fulfillment(of: [released], timeout: 2)
        await viewModel.updateQuery("chrono")
        await slow.value

        guard case .results(let models, _) = viewModel.state else {
            return XCTFail("expected results, got \(viewModel.state)")
        }
        XCTAssertEqual(
            models.map(\.id),
            ["dev-fixture:model-chrono-one", "dev-fixture:model-chrono-two"],
            "the newest query's results must survive; the stale response is dropped"
        )
    }

    func testAnUnchangedQueryIssuesNoRequest() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.updateQuery("chrono")
        XCTAssertEqual(catalog.searchCalls.count, 1)

        await viewModel.updateQuery("  chrono  ")
        XCTAssertEqual(catalog.searchCalls.count, 1, "trimming to the same query is not a new search")
    }

    // MARK: - Paging

    func testLoadMoreAppendsUsingTheServersCursor() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog, pageSize: 2)

        await viewModel.search()
        guard case .results(let first, let canLoadMore) = viewModel.state else {
            return XCTFail("expected results")
        }
        XCTAssertEqual(first.count, 2)
        XCTAssertTrue(canLoadMore)

        await viewModel.loadMore()

        XCTAssertEqual(catalog.searchCalls.count, 2)
        XCTAssertEqual(
            catalog.searchCalls[1].cursor?.id,
            "dev-fixture:model-chrono-two",
            "the second page must continue from the first page's last row"
        )
        guard case .results(let all, let more) = viewModel.state else {
            return XCTFail("expected results")
        }
        XCTAssertEqual(all.count, 3)
        XCTAssertFalse(more)
    }

    func testAFailedAdditionalPageKeepsTheLoadedResults() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog, pageSize: 2)

        await viewModel.search()
        catalog.searchError = URLError(.networkConnectionLost)
        await viewModel.loadMore()

        guard case .results(let models, _) = viewModel.state else {
            return XCTFail("a failed page must not discard loaded results, got \(viewModel.state)")
        }
        XCTAssertEqual(models.count, 2)
        XCTAssertNotNil(viewModel.lastPageErrorMessage)
    }

    // MARK: - Selection, and where it stops

    func testSelectingReturnsTheModelAndPersistsNothing() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.search()
        let selected = viewModel.select(modelID: "dev-fixture:model-diver")

        XCTAssertEqual(selected?.id, "dev-fixture:model-diver")
        XCTAssertEqual(selected?.brandDisplayName, "Testmark (development fixture)")
        XCTAssertEqual(catalog.searchCalls.count, 1)
        XCTAssertEqual(catalog.designFetchCount, 0)
        XCTAssertEqual(catalog.fetchCount, 0)
    }

    func testAModelWithNoAssetIsStillSelectable() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.search()
        let selected = viewModel.select(modelID: "dev-fixture:model-chrono-two")

        XCTAssertNotNil(selected)
        XCTAssertFalse(selected?.hasAsset ?? true)
        XCTAssertNil(selected?.assetBundle, "no bundle reference exists when nothing is authored")
    }

    func testAnUnlistedIDSelectsNothing() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.search()

        XCTAssertNil(viewModel.select(modelID: "model_that_was_never_shown"))
        XCTAssertNil(viewModel.select(modelID: "dev-fixture:model-racer"), "wrong category, never listed")
    }

    func testFixtureModelsCarryTheirClassification() async {
        let catalog = FakeCollectionCatalog()
        let viewModel = makeViewModel(catalog: catalog)

        await viewModel.search()

        guard case .results(let models, _) = viewModel.state else {
            return XCTFail("expected results")
        }
        XCTAssertTrue(models.allSatisfy(\.isDevelopmentFixture))
    }

    func testManualSearchHasNoRecognitionDependency() async {
        let viewModel = CollectionModelSearchViewModel(
            categoryID: "category_watches",
            catalog: FakeCollectionCatalog(),
            accessToken: "token"
        )

        await viewModel.search()

        guard case .results = viewModel.state else {
            return XCTFail("search must work with no ML system present, got \(viewModel.state)")
        }
    }
}
