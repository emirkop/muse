import Foundation

public protocol CapacityPurchasing: Sendable {
    func capacityProduct(id: String) async -> CapacityProduct?

    func purchase(productID: String, appAccountToken: UUID) async -> PurchaseOutcome

    func restore() async -> RestoreOutcome

    func currentEntitlementTransactions() async -> [String]
}

public enum PurchaseOutcome: Equatable, Sendable {
    case purchased(signedTransaction: String)
    case cancelled
    case pending
    case failed(message: String)
}

public enum RestoreOutcome: Equatable, Sendable {
    case restored(signedTransactions: [String])
    case failed(message: String)
}
