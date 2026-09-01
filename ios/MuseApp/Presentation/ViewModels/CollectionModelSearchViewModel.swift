import Foundation

@MainActor
public final class CollectionModelSearchViewModel {

    public enum State: Equatable {
        case searching

        case results(models: [CollectionCatalogModel], canLoadMore: Bool)

        case noResults(query: String)

        case failed(message: String)
    }

    public private(set) var state: State = .searching {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public private(set) var isSearching = false {
        didSet { onActivityChange?(isSearching) }
    }
    public var onActivityChange: ((Bool) -> Void)?

    public let categoryID: String
    public let categoryDisplayName: String?

    public private(set) var query: String

    private let catalog: any CollectionCatalogServicing
    private let accessToken: String
    private let analytics: any AnalyticsRecording
    private let pageSize: Int

    private var generation = 0
    private var loadedModels: [CollectionCatalogModel] = []
    private var nextCursor: CollectionModelSearchCursor?

    public init(
        categoryID: String,
        categoryDisplayName: String? = nil,
        catalog: any CollectionCatalogServicing,
        accessToken: String,
        initialQuery: String = "",
        pageSize: Int = 25,
        analytics: any AnalyticsRecording = NoAnalytics()
    ) {
        self.analytics = analytics
        self.categoryID = categoryID
        self.categoryDisplayName = categoryDisplayName
        self.catalog = catalog
        self.accessToken = accessToken
        self.query = initialQuery
        self.pageSize = pageSize
    }

    public func search() async {
        await runSearch(query: query, reason: .fresh)
    }

    public func updateQuery(_ newQuery: String) async {
        let trimmed = newQuery.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != query else { return }
        query = trimmed
        await runSearch(query: trimmed, reason: .fresh)
    }

    public func loadMore() async {
        guard nextCursor != nil, !isSearching else { return }
        await runSearch(query: query, reason: .more)
    }

    public func select(modelID: String) -> CollectionCatalogModel? {
        let model = loadedModels.first { $0.id == modelID }
        if model != nil {
            analytics.record(.catalogSearchOutcome(.selected, categoryID: categoryID))
        }
        return model
    }

    public func recordSearchAbandoned() {
        analytics.record(.catalogSearchOutcome(.abandoned, categoryID: categoryID))
    }

    // MARK: - Internals

    private enum SearchReason: Equatable {
        case fresh
        case more
    }

    private func runSearch(query searchQuery: String, reason: SearchReason) async {
        generation += 1
        let thisGeneration = generation

        let cursor: CollectionModelSearchCursor?
        switch reason {
        case .fresh:
            cursor = nil
            if loadedModels.isEmpty { state = .searching }
        case .more:
            cursor = nextCursor
        }

        isSearching = true
        defer { if thisGeneration == generation { isSearching = false } }

        do {
            let page = try await catalog.searchCollectionModels(
                accessToken: accessToken,
                categoryID: categoryID,
                query: searchQuery,
                limit: pageSize,
                cursor: cursor
            )

            guard thisGeneration == generation else { return }

            switch reason {
            case .fresh:
                loadedModels = page.models
            case .more:
                loadedModels.append(contentsOf: page.models)
            }
            nextCursor = page.nextCursor

            state = loadedModels.isEmpty
                ? .noResults(query: searchQuery)
                : .results(models: loadedModels, canLoadMore: page.hasMore)
        } catch {
            guard thisGeneration == generation else { return }

            if reason == .more, !loadedModels.isEmpty {
                state = .results(models: loadedModels, canLoadMore: true)
                lastPageErrorMessage = Self.failureMessage(for: error)
                return
            }
            state = .failed(message: Self.failureMessage(for: error))
        }
    }

    public private(set) var lastPageErrorMessage: String?

    private static func failureMessage(for error: Error) -> String {
        if let apiError = error as? CollectionAPIError, (400..<500).contains(apiError.statusCode) {
            return "Couldn't search this collection. Reopen the room and try again."
        }
        return NetworkFailureCopy.message(
            for: error,
            operation: .read,
            otherwise: "Couldn't search the catalog. Check your connection and try again."
        )
    }
}
