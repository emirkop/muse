import Foundation
import StoreKit

public final class AppStoreCapacityStore: CapacityPurchasing, @unchecked Sendable {
    public init() {}

    public func capacityProduct(id: String) async -> CapacityProduct? {
        guard let product = try? await Product.products(for: [id]).first else { return nil }
        return CapacityProduct(id: product.id, displayName: product.displayName, displayPrice: product.displayPrice)
    }

    public func purchase(productID: String, appAccountToken: UUID) async -> PurchaseOutcome {
        guard let product = try? await Product.products(for: [productID]).first else {
            return .failed(message: "This upgrade isn't available right now.")
        }
        do {
            switch try await product.purchase(options: [.appAccountToken(appAccountToken)]) {
            case .success(let verification):
                return .purchased(signedTransaction: verification.jwsRepresentation)
            case .userCancelled:
                return .cancelled
            case .pending:
                return .pending
            @unknown default:
                return .failed(message: "The purchase couldn't be completed.")
            }
        } catch {
            return .failed(message: "The purchase couldn't be completed.")
        }
    }

    public func restore() async -> RestoreOutcome {
        do {
            try await AppStore.sync()
        } catch {
            return .failed(message: "Couldn't reach the App Store to restore purchases.")
        }
        return .restored(signedTransactions: await currentEntitlementTransactions())
    }

    public func currentEntitlementTransactions() async -> [String] {
        var signed: [String] = []
        for await result in Transaction.currentEntitlements {
            signed.append(result.jwsRepresentation)
        }
        return signed
    }

    public func finish(signedTransaction: String) async {
        for await result in Transaction.unfinished where result.jwsRepresentation == signedTransaction {
            await result.unsafePayloadValue.finish()
        }
    }

    public func observeUpdates(_ handler: @escaping @Sendable (String) async -> Void) -> Task<Void, Never> {
        Task.detached {
            for await result in Transaction.updates {
                await handler(result.jwsRepresentation)
            }
        }
    }
}
