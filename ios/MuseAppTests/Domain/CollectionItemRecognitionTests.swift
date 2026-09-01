import XCTest
@testable import MuseApp

final class CollectionItemRecognitionContractTests: XCTestCase {

    // MARK: - The vocabulary

    func test_confidenceIsClampedToZeroThroughOne() {
        XCTAssertEqual(RecognitionConfidence(1.4).value, 1)
        XCTAssertEqual(RecognitionConfidence(-0.2).value, 0)
        XCTAssertEqual(RecognitionConfidence(0.42).value, 0.42)
    }

    func test_candidatesAreOrderedByDescendingConfidence() {
        let candidates = Candidates([
            RecognitionCandidate(catalogModelID: "middle", confidence: .init(0.5)),
            RecognitionCandidate(catalogModelID: "best", confidence: .init(0.9)),
            RecognitionCandidate(catalogModelID: "worst", confidence: .init(0.1))
        ])

        XCTAssertEqual(candidates?.ordered.map(\.catalogModelID), ["best", "middle", "worst"])
        XCTAssertEqual(candidates?.top.catalogModelID, "best")
        XCTAssertEqual(candidates?.count, 3)
    }

    func test_equalConfidencesKeepTheProvidersOrder() {
        let candidates = Candidates([
            RecognitionCandidate(catalogModelID: "first", confidence: .init(0.5)),
            RecognitionCandidate(catalogModelID: "second", confidence: .init(0.5)),
            RecognitionCandidate(catalogModelID: "third", confidence: .init(0.5))
        ])

        XCTAssertEqual(candidates?.ordered.map(\.catalogModelID), ["first", "second", "third"])
    }

    func test_anEmptyCandidateListCannotBeConstructed() {
        XCTAssertNil(Candidates([]))
    }

    func test_bothNonMatchOutcomesRouteToManualSearchFallback() {
        XCTAssertTrue(RecognitionOutcome.noMatch(suggestedQuery: nil).routesToManualSearchFallback)
        XCTAssertTrue(RecognitionOutcome.unavailable(reason: .noProviderConfigured).routesToManualSearchFallback)
        XCTAssertTrue(RecognitionOutcome.unavailable(reason: .attemptFailed).routesToManualSearchFallback)
        XCTAssertTrue(RecognitionOutcome.unavailable(reason: .inputUnavailable).routesToManualSearchFallback)

        let candidates = Candidates([RecognitionCandidate(catalogModelID: "m", confidence: .init(0.5))])!
        XCTAssertFalse(RecognitionOutcome.candidates(candidates).routesToManualSearchFallback)
    }

    func test_theSuggestedQueryIsCarriedOnlyWhenAProviderSuppliedOne() {
        XCTAssertEqual(
            RecognitionOutcome.noMatch(suggestedQuery: "devco").suggestedManualSearchQuery, "devco"
        )
        XCTAssertNil(RecognitionOutcome.noMatch(suggestedQuery: nil).suggestedManualSearchQuery)
        XCTAssertNil(
            RecognitionOutcome.unavailable(reason: .attemptFailed).suggestedManualSearchQuery
        )
    }

    // MARK: - The DEV mock

    private func makeMock(
        behaviour: DevMockCollectionItemRecognizer.Behaviour = .candidatesFromCatalog(count: 3),
        catalog: FakeCollectionCatalog = FakeCollectionCatalog(),
        environment: AppEnvironment = .development
    ) -> DevMockCollectionItemRecognizer? {
        DevMockCollectionItemRecognizer.make(
            catalog: catalog, accessToken: { "token" }, behaviour: behaviour, environment: environment
        )
    }

    private func input(categoryID: String = "category_watches") -> RecognitionInput {
        RecognitionInput(
            imageFileURL: URL(fileURLWithPath: "/dev/null/not-a-real-capture.jpg"),
            categoryID: categoryID
        )
    }

    func test_theMockCannotBeConstructedOutsideDevelopment() {
        XCTAssertNil(makeMock(environment: .production))
        XCTAssertNil(makeMock(environment: .staging))
        XCTAssertNotNil(makeMock(environment: .development))
    }

    func test_theMockNeverReadsTheImageAndIsDeterministic() async throws {
        let mock = try XCTUnwrap(makeMock())

        let first = await mock.recognize(input())
        let second = await mock.recognize(
            RecognitionInput(
                imageFileURL: URL(fileURLWithPath: "/tmp/some-other-file-that-does-not-exist.heic"),
                categoryID: "category_watches"
            )
        )

        XCTAssertEqual(first, second, "the answer must not depend on the image at all")
        guard case .candidates(let candidates) = first else {
            return XCTFail("expected candidates, got \(first)")
        }
        XCTAssertEqual(candidates.count, 3)
    }

    func test_theMockReturnsSeededDevCatalogModelsForTheRequestedCategory() async throws {
        let mock = try XCTUnwrap(makeMock())

        let outcome = await mock.recognize(input(categoryID: "category_watches"))

        guard case .candidates(let candidates) = outcome else {
            return XCTFail("expected candidates, got \(outcome)")
        }
        let seededWatchIDs = FakeCollectionCatalog.seededModels
            .filter { $0.categoryID == "category_watches" }
            .map(\.id)
        for candidate in candidates.ordered {
            XCTAssertTrue(
                seededWatchIDs.contains(candidate.catalogModelID),
                "\(candidate.catalogModelID) is not a seeded Watches DEV Model"
            )
            XCTAssertTrue(
                candidate.catalogModelID.hasPrefix("dev-fixture:"),
                "a mock must only ever return clearly-labelled development fixture Models"
            )
        }
        XCTAssertFalse(candidates.ordered.contains { $0.catalogModelID == "dev-fixture:model-racer" })
    }

    func test_theMockConfidencesAreFixedPositionalPlaceholders() async throws {
        let mock = try XCTUnwrap(makeMock())

        let outcome = await mock.recognize(input())

        guard case .candidates(let candidates) = outcome else {
            return XCTFail("expected candidates, got \(outcome)")
        }
        XCTAssertEqual(
            candidates.ordered.map(\.confidence.value),
            Array(DevMockCollectionItemRecognizer.positionalPlaceholderConfidences.prefix(candidates.count))
        )
        for (earlier, later) in zip(candidates.ordered, candidates.ordered.dropFirst()) {
            XCTAssertGreaterThan(earlier.confidence, later.confidence)
        }
    }

    func test_theMockCanProduceEveryBranchOfTheFlow() async throws {
        let single = try XCTUnwrap(makeMock(behaviour: .singleCandidateFromCatalog))
        guard case .candidates(let one) = await single.recognize(input()) else {
            return XCTFail("expected one candidate")
        }
        XCTAssertEqual(one.count, 1)

        let fallback = try XCTUnwrap(makeMock(behaviour: .noMatch(suggestedQuery: "devco")))
        let fallbackOutcome = await fallback.recognize(input())
        XCTAssertEqual(fallbackOutcome, .noMatch(suggestedQuery: "devco"))

        let unavailable = try XCTUnwrap(makeMock(behaviour: .unavailable(.attemptFailed)))
        let unavailableOutcome = await unavailable.recognize(input())
        XCTAssertEqual(unavailableOutcome, .unavailable(reason: .attemptFailed))
    }

    func test_anEmptyCatalogYieldsNoMatchWithNoInventedSignal() async throws {
        let empty = FakeCollectionCatalog()
        empty.models = []
        let mock = try XCTUnwrap(makeMock(catalog: empty))

        let outcome = await mock.recognize(input())
        XCTAssertEqual(outcome, .noMatch(suggestedQuery: nil))
    }

    func test_aCatalogFailureBecomesUnavailableRatherThanAnError() async throws {
        let failing = FakeCollectionCatalog()
        failing.searchError = CollectionAPIError(statusCode: 500, code: nil, message: nil)
        let mock = try XCTUnwrap(makeMock(catalog: failing))

        let outcome = await mock.recognize(input())

        XCTAssertEqual(outcome, .unavailable(reason: .attemptFailed))
        XCTAssertTrue(outcome.routesToManualSearchFallback)
    }

    // MARK: - Replaceability, and Manual Search's independence

    func test_anyProviderSatisfiesTheSameContract() async {
        struct StubRecognizer: CollectionItemRecognizing {
            func recognize(_ input: RecognitionInput) async -> RecognitionOutcome {
                .candidates(Candidates([
                    RecognitionCandidate(catalogModelID: "stub-model", confidence: .init(0.77))
                ])!)
            }
        }

        let providers: [any CollectionItemRecognizing] = [
            StubRecognizer(),
            DevMockCollectionItemRecognizer.make(
                catalog: FakeCollectionCatalog(), accessToken: { "token" }, environment: .development
            )!
        ]
        for provider in providers {
            let outcome = await provider.recognize(input())
            switch outcome {
            case .candidates(let candidates):
                XCTAssertFalse(candidates.ordered.isEmpty)
            case .noMatch, .unavailable:
                XCTAssertTrue(outcome.routesToManualSearchFallback)
            }
        }
    }

    @MainActor
    func test_manualSearchWorksWithNoRecognitionProviderInExistence() async {
        let catalog = FakeCollectionCatalog()
        let search = CollectionModelSearchViewModel(
            categoryID: "category_watches", catalog: catalog, accessToken: "token"
        )

        await search.search()

        guard case .results(let models, _) = search.state else {
            return XCTFail("expected results, got \(search.state)")
        }
        XCTAssertFalse(models.isEmpty)
    }

    @MainActor
    func test_theFallbackSignalReachesManualSearchAsPlainText() async {
        let outcome = RecognitionOutcome.noMatch(suggestedQuery: "devco chrono")

        let search = CollectionModelSearchViewModel(
            categoryID: "category_watches",
            catalog: FakeCollectionCatalog(),
            accessToken: "token",
            initialQuery: outcome.suggestedManualSearchQuery ?? ""
        )

        XCTAssertEqual(search.query, "devco chrono")
    }
}
