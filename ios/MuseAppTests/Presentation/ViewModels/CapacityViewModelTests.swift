import XCTest
@testable import MuseApp

@MainActor
final class CapacityViewModelTests: XCTestCase {
    private let productID = "dev.muse.placeholder.collection_capacity"

    private func makeViewModel(_ service: FakeEntitlementService, _ store: FakeCapacityStore) -> CapacityViewModel {
        CapacityViewModel(
            entitlements: service, store: store, productID: productID, accessToken: "token",
            finish: { [store] signed in await store.finish(signedTransaction: signed) }
        )
    }

    private let paid = AccountEntitlement(state: .paid, itemCapacity: 6, itemCount: 3)

    func test_load_showsTheServersNumbersAndTheStoresProduct() async {
        let service = FakeEntitlementService()
        let store = FakeCapacityStore()
        let viewModel = makeViewModel(service, store)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready(entitlement: service.entitlement, product: store.product))
        XCTAssertEqual(CapacityViewModel.capacityMessage(service.entitlement), "You've used all 3 free item slots across your collections.")
    }

    func test_load_withoutAStoreProduct_offersNoUpgrade() async {
        let service = FakeEntitlementService()
        let store = FakeCapacityStore()
        store.product = nil
        let viewModel = makeViewModel(service, store)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .ready(entitlement: service.entitlement, product: nil))
        await viewModel.purchase()
        XCTAssertTrue(store.purchaseCalls.isEmpty, "nothing to buy, nothing bought")
    }

    func test_purchase_bindsWithTheAccountToken_redeemsTheSignedTransaction_thenFinishes() async {
        let service = FakeEntitlementService()
        service.acceptedTransactions["jws-purchase"] = paid
        let store = FakeCapacityStore()
        let viewModel = makeViewModel(service, store)
        await viewModel.load()

        await viewModel.purchase()

        XCTAssertEqual(store.purchaseCalls.count, 1)
        XCTAssertEqual(store.purchaseCalls[0].productID, productID)
        XCTAssertEqual(store.purchaseCalls[0].appAccountToken, service.token, "the purchase carries the account's binding token")
        XCTAssertEqual(service.redeemed, ["jws-purchase"], "the server receives signed bytes, never a flag")
        XCTAssertEqual(viewModel.state, .upgraded(paid))
        XCTAssertEqual(store.finished, ["jws-purchase"], "finished only after the server accepted it")
    }

    func test_cancelledPurchase_changesNothing() async {
        let service = FakeEntitlementService()
        let store = FakeCapacityStore()
        store.purchaseOutcome = .cancelled
        let viewModel = makeViewModel(service, store)
        await viewModel.load()

        await viewModel.purchase()

        XCTAssertEqual(viewModel.state, .ready(entitlement: service.entitlement, product: store.product))
        XCTAssertTrue(service.redeemed.isEmpty)
        XCTAssertTrue(store.finished.isEmpty)
    }

    func test_serverRefusal_neverProducesAPaidState_andDoesNotFinish() async {
        let service = FakeEntitlementService()
        let store = FakeCapacityStore()
        store.purchaseOutcome = .purchased(signedTransaction: "jws-unknown")
        let viewModel = makeViewModel(service, store)
        await viewModel.load()

        await viewModel.purchase()

        guard case .failed(_, let entitlement) = viewModel.state else { return XCTFail("expected failed, got \(viewModel.state)") }
        XCTAssertEqual(entitlement?.state, .free)
        XCTAssertTrue(store.finished.isEmpty, "an unaccepted transaction must stay unfinished")
    }

    func test_transactionBoundToAnotherAccount_isReportedAsSuch() async {
        let service = FakeEntitlementService()
        service.boundElsewhere = ["jws-purchase"]
        let store = FakeCapacityStore()
        let viewModel = makeViewModel(service, store)
        await viewModel.load()

        await viewModel.purchase()

        XCTAssertEqual(viewModel.state, .failed(message: CapacityViewModel.boundToAnotherAccountMessage, entitlement: service.entitlement))
        XCTAssertTrue(store.finished.isEmpty)
    }

    func test_verificationUnavailable_leavesThePurchaseUnfinished_andNotPaid() async {
        let service = FakeEntitlementService()
        service.redeemUnavailable = true
        let store = FakeCapacityStore()
        let viewModel = makeViewModel(service, store)
        await viewModel.load()

        await viewModel.purchase()

        guard case .failed(let message, let entitlement) = viewModel.state else { return XCTFail("\(viewModel.state)") }
        XCTAssertTrue(message.contains("automatically"))
        XCTAssertEqual(entitlement?.state, .free)
        XCTAssertTrue(store.finished.isEmpty)
    }

    func test_restore_redeemsEveryCurrentEntitlement_andAdoptsTheServersState() async {
        let service = FakeEntitlementService()
        service.acceptedTransactions["jws-old"] = paid
        let store = FakeCapacityStore()
        store.restoreOutcome = .restored(signedTransactions: ["jws-old"])
        let viewModel = makeViewModel(service, store)
        await viewModel.load()

        await viewModel.restore()

        XCTAssertEqual(store.restoreCalls, 1)
        XCTAssertEqual(service.redeemed, ["jws-old"])
        XCTAssertEqual(viewModel.state, .upgraded(paid))
        XCTAssertEqual(store.finished, ["jws-old"])
    }

    func test_restore_ofAnotherAccountsPurchase_doesNotUnlock() async {
        let service = FakeEntitlementService()
        service.boundElsewhere = ["jws-of-account-a"]
        let store = FakeCapacityStore()
        store.restoreOutcome = .restored(signedTransactions: ["jws-of-account-a"])
        let viewModel = makeViewModel(service, store)
        await viewModel.load()

        await viewModel.restore()

        XCTAssertEqual(viewModel.state, .failed(message: CapacityViewModel.boundToAnotherAccountMessage, entitlement: service.entitlement))
        XCTAssertEqual(service.entitlement.state, .free)
        XCTAssertTrue(store.finished.isEmpty)
    }

    func test_restore_withNothingToRestore_saysSo() async {
        let service = FakeEntitlementService()
        let store = FakeCapacityStore()
        let viewModel = makeViewModel(service, store)
        await viewModel.load()

        await viewModel.restore()

        guard case .failed(let message, _) = viewModel.state else { return XCTFail("\(viewModel.state)") }
        XCTAssertTrue(message.contains("No purchase to restore"))
    }

    func test_revokedCopy_saysItemsAreKept() {
        let revoked = AccountEntitlement(state: .revoked, itemCapacity: 3, itemCount: 5)
        let message = CapacityViewModel.capacityMessage(revoked)
        XCTAssertTrue(message.contains("keep all 5 items"))
        XCTAssertTrue(revoked.isAtCapacity)
        XCTAssertTrue(revoked.canUpgrade, "a refunded account may purchase again")
        XCTAssertFalse(paid.canUpgrade, "one paid tier: nothing more to buy")
    }
}
