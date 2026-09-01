import Foundation

@MainActor
public final class CapacityViewModel {
    public enum State: Equatable {
        case loading
        case ready(entitlement: AccountEntitlement, product: CapacityProduct?)
        case purchasing
        case restoring
        case upgraded(AccountEntitlement)
        case failed(message: String, entitlement: AccountEntitlement?)
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let entitlements: any EntitlementServicing
    private let store: any CapacityPurchasing
    private let productID: String
    private let analytics: any AnalyticsRecording
    private let accessToken: String
    private let finish: @Sendable (String) async -> Void
    private var lastEntitlement: AccountEntitlement?
    private var lastProduct: CapacityProduct?

    public var retryableProduct: CapacityProduct? { lastProduct }

    public init(
        entitlements: any EntitlementServicing,
        store: any CapacityPurchasing,
        productID: String,
        accessToken: String,
        finish: @escaping @Sendable (String) async -> Void = { _ in },
        analytics: any AnalyticsRecording = NoAnalytics()
    ) {
        self.analytics = analytics
        self.entitlements = entitlements
        self.store = store
        self.productID = productID
        self.accessToken = accessToken
        self.finish = finish
    }

    public func load() async {
        state = .loading
        do {
            let entitlement = try await entitlements.fetchEntitlement(accessToken: accessToken)
            lastEntitlement = entitlement
            let product = await store.capacityProduct(id: productID)
            lastProduct = product
            state = .ready(entitlement: entitlement, product: product)
            analytics.record(.capacityUpgradeStep(.capacityScreenShown))
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't check your collection capacity. Please try again."), entitlement: nil)
        }
    }

    public func purchase() async {
        guard case .ready(let entitlement, let product) = state, product != nil else { return }
        state = .purchasing
        analytics.record(.capacityUpgradeStep(.purchaseStarted))
        let token: UUID
        do {
            token = try await entitlements.appAccountToken(accessToken: accessToken)
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: "Couldn't prepare the purchase. Please try again."), entitlement: entitlement)
            analytics.record(.capacityUpgradeStep(.purchaseFailed))
            analytics.record(.failureShown(
                surface: .capacity, classification: .of(error), retried: false, retrySucceeded: false))
            return
        }
        switch await store.purchase(productID: productID, appAccountToken: token) {
        case .cancelled:
            state = .ready(entitlement: entitlement, product: product)
        case .pending:
            state = .failed(message: "Your purchase is awaiting approval. Your capacity will update once it's approved.", entitlement: entitlement)
        case .failed(let message):
            state = .failed(message: message, entitlement: entitlement)
            analytics.record(.capacityUpgradeStep(.purchaseFailed))
        case .purchased(let signedTransaction):
            await redeem(signedTransaction, fallback: entitlement)
        }
    }

    public func restore() async {
        guard case .ready(let entitlement, let product) = state else { return }
        state = .restoring
        switch await store.restore() {
        case .failed(let message):
            state = .failed(message: message, entitlement: entitlement)
        case .restored(let signedTransactions):
            if signedTransactions.isEmpty {
                state = .failed(message: "No purchase to restore was found for this Apple ID.", entitlement: entitlement)
                return
            }
            var latest = entitlement
            var boundElsewhere = false
            for signed in signedTransactions {
                do {
                    latest = try await entitlements.redeem(accessToken: accessToken, signedTransaction: signed)
                    await finish(signed)
                } catch let error as EntitlementAPIError where error.isBoundToAnotherAccount {
                    boundElsewhere = true
                } catch {
                    continue
                }
            }
            lastEntitlement = latest
            if latest.state == .paid {
                state = .upgraded(latest)
            } else if boundElsewhere {
                state = .failed(message: Self.boundToAnotherAccountMessage, entitlement: latest)
            } else {
                state = .ready(entitlement: latest, product: product)
            }
        }
    }

    private func redeem(_ signedTransaction: String, fallback: AccountEntitlement) async {
        do {
            let entitlement = try await entitlements.redeem(accessToken: accessToken, signedTransaction: signedTransaction)
            lastEntitlement = entitlement
            await finish(signedTransaction)
            state = entitlement.state == .paid
                ? .upgraded(entitlement)
                : .failed(message: "The purchase was recorded but didn't unlock capacity. Please contact support.", entitlement: entitlement)
        } catch let error as EntitlementAPIError where error.isBoundToAnotherAccount {
            state = .failed(message: Self.boundToAnotherAccountMessage, entitlement: fallback)
        } catch let error as EntitlementAPIError where error.isVerificationUnavailable {
            state = .failed(message: "Couldn't confirm the purchase with Muse yet. It will be applied automatically.", entitlement: fallback)
        } catch {
            state = .failed(message: "Couldn't confirm the purchase. It will be retried automatically.", entitlement: fallback)
        }
    }

    // MARK: - Copy

    public static let boundToAnotherAccountMessage =
        "This purchase belongs to a different Muse account. Sign in to that account to use it."

    public static func capacityMessage(_ e: AccountEntitlement) -> String {
        switch e.state {
        case .paid:
            return e.isAtCapacity
                ? "You've used all \(e.itemCapacity) item slots included with your upgrade."
                : "You're using \(e.itemCount) of \(e.itemCapacity) item slots."
        case .revoked:
            return "Your upgrade was refunded. Your collections keep all \(e.itemCount) items, and you can add more once you're within the \(e.itemCapacity) free slots — or upgrade again."
        case .unavailable, .unknown:
            return "Your collection capacity couldn't be confirmed right now."
        case .free:
            return e.isAtCapacity
                ? "You've used all \(e.itemCapacity) free item slots across your collections."
                : "You're using \(e.itemCount) of \(e.itemCapacity) free item slots."
        }
    }
}
