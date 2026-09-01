import Foundation

public struct AssetCachePolicy: Equatable, Sendable {
    public let budgetBytes: Int64

    public init(budgetBytes: Int64) {
        precondition(budgetBytes > 0, "a cache budget must be positive")
        self.budgetBytes = budgetBytes
    }

    public static let developmentDefault = AssetCachePolicy(budgetBytes: 512 * 1024 * 1024)
}
