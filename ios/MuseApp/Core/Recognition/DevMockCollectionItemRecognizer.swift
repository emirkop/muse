import Foundation

public struct DevMockCollectionItemRecognizer: CollectionItemRecognizing {

    public enum Behaviour: Equatable, Sendable {
        case candidatesFromCatalog(count: Int)
        case singleCandidateFromCatalog
        case noMatch(suggestedQuery: String?)
        case unavailable(RecognitionOutcome.UnavailableReason)
    }

    static let positionalPlaceholderConfidences: [Double] = [0.9, 0.6, 0.3]

    private let catalog: any CollectionCatalogServicing
    private let accessToken: @Sendable () async -> String?
    private let behaviour: Behaviour

    public static func make(
        catalog: any CollectionCatalogServicing,
        accessToken: @escaping @Sendable () async -> String?,
        behaviour: Behaviour = .candidatesFromCatalog(count: 3),
        environment: AppEnvironment = AppEnvironment.current
    ) -> DevMockCollectionItemRecognizer? {
        guard environment == .development else { return nil }
        return DevMockCollectionItemRecognizer(
            catalog: catalog, accessToken: accessToken, behaviour: behaviour
        )
    }

    private init(
        catalog: any CollectionCatalogServicing,
        accessToken: @escaping @Sendable () async -> String?,
        behaviour: Behaviour
    ) {
        self.catalog = catalog
        self.accessToken = accessToken
        self.behaviour = behaviour
    }

    public func recognize(_ input: RecognitionInput) async -> RecognitionOutcome {
        switch behaviour {
        case .noMatch(let suggestedQuery):
            return .noMatch(suggestedQuery: suggestedQuery)
        case .unavailable(let reason):
            return .unavailable(reason: reason)
        case .singleCandidateFromCatalog:
            return await catalogCandidates(categoryID: input.categoryID, count: 1)
        case .candidatesFromCatalog(let count):
            return await catalogCandidates(categoryID: input.categoryID, count: count)
        }
    }

    private func catalogCandidates(categoryID: String, count: Int) async -> RecognitionOutcome {
        guard let token = await accessToken() else {
            return .unavailable(reason: .attemptFailed)
        }
        let requested = min(max(count, 1), Self.positionalPlaceholderConfidences.count)
        let page: CollectionModelSearchPage
        do {
            page = try await catalog.searchCollectionModels(
                accessToken: token, categoryID: categoryID, query: "", limit: requested, cursor: nil
            )
        } catch {
            return .unavailable(reason: .attemptFailed)
        }

        let candidates = page.models.prefix(requested).enumerated().map { position, model in
            RecognitionCandidate(
                catalogModelID: model.id,
                confidence: RecognitionConfidence(Self.positionalPlaceholderConfidences[position])
            )
        }
        guard let ordered = Candidates(Array(candidates)) else {
            return .noMatch(suggestedQuery: nil)
        }
        return .candidates(ordered)
    }
}
