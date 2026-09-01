import Foundation
@testable import MuseApp

final class FakeEntitlementService: EntitlementServicing, @unchecked Sendable {
    var entitlement = AccountEntitlement(state: .free, itemCapacity: 3, itemCount: 3)
    var token = UUID()
    var acceptedTransactions: [String: AccountEntitlement] = [:]
    var boundElsewhere: Set<String> = []
    var fetchError: Error?
    var tokenError: Error?
    var redeemUnavailable = false

    private(set) var fetchCalls = 0
    private(set) var tokenCalls = 0
    private(set) var redeemed: [String] = []

    func fetchEntitlement(accessToken: String) async throws -> AccountEntitlement {
        fetchCalls += 1
        if let fetchError { throw fetchError }
        return entitlement
    }

    func appAccountToken(accessToken: String) async throws -> UUID {
        tokenCalls += 1
        if let tokenError { throw tokenError }
        return token
    }

    func redeem(accessToken: String, signedTransaction: String) async throws -> AccountEntitlement {
        redeemed.append(signedTransaction)
        if redeemUnavailable {
            throw EntitlementAPIError(statusCode: 503, code: "verification_unavailable", message: "unavailable")
        }
        if boundElsewhere.contains(signedTransaction) {
            throw EntitlementAPIError(statusCode: 409, code: "app_account_token_mismatch", message: "another account")
        }
        guard let result = acceptedTransactions[signedTransaction] else {
            throw EntitlementAPIError(statusCode: 400, code: "invalid_signed_transaction", message: "did not verify")
        }
        entitlement = result
        return result
    }
}

final class FakeCapacityStore: CapacityPurchasing, @unchecked Sendable {
    var product: CapacityProduct? = CapacityProduct(id: "dev.muse.placeholder.collection_capacity", displayName: "DEV PLACEHOLDER", displayPrice: "$0.00")
    var purchaseOutcome: PurchaseOutcome = .purchased(signedTransaction: "jws-purchase")
    var restoreOutcome: RestoreOutcome = .restored(signedTransactions: [])
    var currentEntitlements: [String] = []

    private(set) var purchaseCalls: [(productID: String, appAccountToken: UUID)] = []
    private(set) var restoreCalls = 0
    private(set) var finished: [String] = []

    func capacityProduct(id: String) async -> CapacityProduct? { product?.id == id ? product : nil }

    func purchase(productID: String, appAccountToken: UUID) async -> PurchaseOutcome {
        purchaseCalls.append((productID, appAccountToken))
        return purchaseOutcome
    }

    func restore() async -> RestoreOutcome {
        restoreCalls += 1
        return restoreOutcome
    }

    func currentEntitlementTransactions() async -> [String] { currentEntitlements }

    func finish(signedTransaction: String) async { finished.append(signedTransaction) }
}
