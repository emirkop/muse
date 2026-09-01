import Foundation

public protocol CollectionCatalogServicing: Sendable {
    func fetchCollectionCategories(accessToken: String) async throws -> [CollectionCategory]

    func fetchCollectionDesigns(
        accessToken: String,
        categoryID: String
    ) async throws -> [CollectionDesign]

    func searchCollectionModels(
        accessToken: String,
        categoryID: String,
        query: String,
        limit: Int,
        cursor: CollectionModelSearchCursor?
    ) async throws -> CollectionModelSearchPage
}
