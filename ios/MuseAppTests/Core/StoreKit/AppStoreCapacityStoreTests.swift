import StoreKit
import StoreKitTest
import XCTest
@testable import MuseApp

@MainActor
final class AppStoreCapacityStoreTests: XCTestCase {
    private var session: SKTestSession!
    private let productID = "dev.muse.placeholder.collection_capacity"

    override func setUpWithError() throws {
        try super.setUpWithError()
        let url = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "MusePlaceholder", withExtension: "storekit"),
                                "the DEV PLACEHOLDER StoreKit configuration must be in the test bundle")
        session = try SKTestSession(contentsOf: url)
        session.resetToDefaultState()
        session.disableDialogs = true
        session.clearTransactions()
        session.storefront = "USA"
        try XCTSkipIf(session.storefront.isEmpty,
                      "StoreKit test environment not live in this host (SKTestSession storefront empty) — AppStoreCapacityStore is compile-verified here, not exercised")
    }

    override func tearDown() {
        session?.clearTransactions()
        session = nil
        super.tearDown()
    }

    func test_capacityProduct_isTheConfiguredPlaceholder_andNothingElse() async {
        let store = AppStoreCapacityStore()

        let product = await store.capacityProduct(id: productID)
        let missing = await store.capacityProduct(id: "com.muse.not.configured")

        XCTAssertEqual(product?.id, productID)
        XCTAssertEqual(product?.displayName, "DEV PLACEHOLDER Collection Capacity")
        XCTAssertNil(missing, "an id the store does not know yields no product — nothing is invented client-side")
    }

    func test_purchase_returnsASignedTransaction_carryingTheAppAccountToken() async throws {
        let store = AppStoreCapacityStore()
        let token = UUID()

        let outcome = await store.purchase(productID: productID, appAccountToken: token)

        guard case .purchased(let signed) = outcome else { return XCTFail("expected a purchase, got \(outcome)") }
        let parts = signed.split(separator: ".")
        XCTAssertEqual(parts.count, 3, "a JWS: header.payload.signature")
        var payloadB64 = String(parts[1]).replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while payloadB64.count % 4 != 0 { payloadB64 += "=" }
        let payload = try JSONSerialization.jsonObject(with: try XCTUnwrap(Data(base64Encoded: payloadB64))) as? [String: Any]
        XCTAssertEqual(payload?["productId"] as? String, productID)
        XCTAssertEqual((payload?["appAccountToken"] as? String)?.lowercased(), token.uuidString.lowercased(), "the binding token travels inside the signed payload")
        XCTAssertEqual(payload?["type"] as? String, "Non-Consumable")
        XCTAssertEqual(payload?["inAppOwnershipType"] as? String, "PURCHASED")

        let current = await store.currentEntitlementTransactions()
        XCTAssertEqual(current.count, 1)
        await store.finish(signedTransaction: signed)
    }

    func test_theClientOffersNoLocalVerdict() {
        let store: any CapacityPurchasing = AppStoreCapacityStore()
        XCTAssertFalse(store is EntitlementServicing, "the store cannot answer for the server")
    }
}
