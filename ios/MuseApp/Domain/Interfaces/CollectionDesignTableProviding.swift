import Foundation

public protocol CollectionDesignTableProviding: Sendable {
    func tierTable(
        accessToken: String,
        categoryID: String,
        designID: String
    ) async -> CollectionTierTable?
}
