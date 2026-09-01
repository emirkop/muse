import XCTest
@testable import MuseApp

final class CollectionItemAdditionTests: XCTestCase {

    private let table = fixtureTierTable

    private func room(itemCount: Int, tier: Int = 1) -> CollectionRoom {
        CollectionRoom(
            id: "c1", name: "Watches",
            categoryID: "category_watches", designID: "dev-fixture:collection-design",
            currentTier: CollectionTier(tier),
            items: (0..<itemCount).map {
                CollectionItem(id: "existing-\($0)", slotIndex: $0, catalogModelID: "model")
            }
        )
    }

    private func makeAddition(
        room: CollectionRoom
    ) -> (CollectionItemAddition, FakeTierRatchet, FakeTierGeometry, FakeCollectionItemStore) {
        let ratchet = FakeTierRatchet()
        ratchet.storedTier = room.currentTier
        let geometry = FakeTierGeometry()
        let store = FakeCollectionItemStore(room: room)
        let addition = CollectionItemAddition(
            expansion: CollectionTierExpansion(ratchet: ratchet, geometry: geometry),
            items: store
        )
        return (addition, ratchet, geometry, store)
    }

    // MARK: - Verify 1: a selected Model becomes a real item

    func test_addingAModelCreatesAnItemAndReportsItsSlot() async {
        let start = room(itemCount: 0)
        let (addition, ratchet, geometry, store) = makeAddition(room: start)

        let result = await addition.add(
            catalogModelID: "dev-fixture:model-chrono-one",
            to: start, table: table, accessToken: "t"
        )

        guard case .success(let outcome) = result else {
            return XCTFail("expected success, got \(result)")
        }
        XCTAssertEqual(outcome.placedItem.slotIndex, 0)
        XCTAssertEqual(outcome.placedItem.catalogModelID, "dev-fixture:model-chrono-one")
        XCTAssertEqual(outcome.room.itemCount, 1)
        XCTAssertEqual(store.addCalls, [
            .init(collectionRoomID: "c1", catalogModelID: "dev-fixture:model-chrono-one")
        ])
        XCTAssertNil(outcome.expansion)
        XCTAssertTrue(ratchet.requests.isEmpty, "an addition inside capacity must not ratchet")
        XCTAssertTrue(geometry.installed.isEmpty, "an addition inside capacity must fetch nothing")
    }

    func test_theNewItemIsIdentifiedByDifferenceNotByPosition() async {
        let start = CollectionRoom(
            id: "c1", name: "Watches",
            categoryID: "category_watches", designID: "dev-fixture:collection-design",
            currentTier: .base,
            items: [
                CollectionItem(id: "existing-1", slotIndex: 1, catalogModelID: "model"),
                CollectionItem(id: "existing-2", slotIndex: 2, catalogModelID: "model"),
                CollectionItem(id: "existing-3", slotIndex: 3, catalogModelID: "model")
            ]
        )
        let (addition, _, _, _) = makeAddition(room: start)

        let result = await addition.add(
            catalogModelID: "model", to: start, table: table, accessToken: "t"
        )

        guard case .success(let outcome) = result else {
            return XCTFail("expected success, got \(result)")
        }
        XCTAssertEqual(outcome.placedItem.slotIndex, 0, "the hole should be filled first")
        XCTAssertFalse(
            ["existing-1", "existing-2", "existing-3"].contains(outcome.placedItem.id),
            "an existing item was reported as the new one"
        )
    }

    // MARK: - Verify 4: crossing a capacity ratchets FIRST

    func test_crossingACapacityExpandsBeforePlacing() async {
        let start = room(itemCount: 4)
        let (addition, ratchet, geometry, store) = makeAddition(room: start)

        let result = await addition.add(
            catalogModelID: "model", to: start, table: table, accessToken: "t"
        )

        guard case .success(let outcome) = result else {
            return XCTFail("expected success, got \(result)")
        }
        XCTAssertEqual(ratchet.requests, [2])
        XCTAssertEqual(outcome.expansion?.tier, CollectionTier(2))
        XCTAssertEqual(outcome.expansion?.expanded, true)
        XCTAssertEqual(geometry.installed.map(\.id), ["bundle_tier2"])
        XCTAssertEqual(outcome.placedItem.slotIndex, 4)
        XCTAssertEqual(store.addCalls.count, 1)
    }

    func test_geometryIsInstalledBeforeTheItemIsCreated() async {
        let start = room(itemCount: 4)
        let ratchet = FakeTierRatchet()
        ratchet.storedTier = start.currentTier
        let geometry = FakeTierGeometry()
        let store = FakeCollectionItemStore(room: start)

        var order: [String] = []
        store.gate = { order.append("add") }

        let addition = CollectionItemAddition(
            expansion: CollectionTierExpansion(ratchet: ratchet, geometry: geometry),
            items: store
        )
        _ = await addition.add(catalogModelID: "model", to: start, table: table, accessToken: "t")

        XCTAssertEqual(geometry.installed.count, 1, "tier 2's geometry should have been installed")
        XCTAssertEqual(order, ["add"], "the add should have happened exactly once")
        XCTAssertFalse(geometry.installed.isEmpty)
    }

    func test_jumpingSeveralTiersInstallsOnlyTheCrossedOnes() async {
        let start = room(itemCount: 10, tier: 2)
        let (addition, ratchet, geometry, _) = makeAddition(room: start)

        _ = await addition.add(catalogModelID: "model", to: start, table: table, accessToken: "t")

        XCTAssertEqual(ratchet.requests, [3])
        XCTAssertEqual(geometry.installed.map(\.id), ["bundle_tier3"])
    }

    func test_aGeometryFailureStillPlacesTheItemAndIsReported() async {
        let start = room(itemCount: 4)
        let ratchet = FakeTierRatchet()
        ratchet.storedTier = start.currentTier
        let geometry = FakeTierGeometry()
        geometry.failing = ["bundle_tier2"]
        let store = FakeCollectionItemStore(room: start)
        let addition = CollectionItemAddition(
            expansion: CollectionTierExpansion(ratchet: ratchet, geometry: geometry),
            items: store
        )

        let result = await addition.add(
            catalogModelID: "model", to: start, table: table, accessToken: "t"
        )

        guard case .success(let outcome) = result else {
            return XCTFail("expected success, got \(result)")
        }
        XCTAssertTrue(outcome.newGeometryIncomplete)
        XCTAssertEqual(outcome.placedItem.slotIndex, 4)
    }

    func test_aRefusedRatchetPlacesNothing() async {
        let start = room(itemCount: 4)
        let ratchet = FakeTierRatchet()
        ratchet.storedTier = start.currentTier
        ratchet.failure = CollectionAPIError(statusCode: 500, code: nil, message: nil)
        let store = FakeCollectionItemStore(room: start)
        let addition = CollectionItemAddition(
            expansion: CollectionTierExpansion(ratchet: ratchet, geometry: FakeTierGeometry()),
            items: store
        )

        let result = await addition.add(
            catalogModelID: "model", to: start, table: table, accessToken: "t"
        )

        XCTAssertEqual(result, .failure(.expansionFailed(.ratchetRefused)))
        XCTAssertTrue(store.addCalls.isEmpty, "nothing may be placed when expansion failed")
    }

    // MARK: - Verify 2: an unknown Model is rejected

    func test_aModelTheServerWillNotVouchForIsReportedAsUnavailable() async {
        let start = room(itemCount: 0)
        let (addition, _, _, store) = makeAddition(room: start)
        store.addError = CollectionAPIError(
            statusCode: 400, code: "model_not_available", message: "not available"
        )

        let result = await addition.add(
            catalogModelID: "dev-fixture:model-does-not-exist",
            to: start, table: table, accessToken: "t"
        )

        XCTAssertEqual(result, .failure(.modelNotAvailable))
    }

    func test_aMissingRoomIsReportedSeparately() async {
        let start = room(itemCount: 0)
        let (addition, _, _, store) = makeAddition(room: start)
        store.addError = CollectionAPIError(statusCode: 404, code: nil, message: "not found")

        let result = await addition.add(
            catalogModelID: "model", to: start, table: table, accessToken: "t"
        )

        XCTAssertEqual(result, .failure(.roomUnavailable))
    }

    func test_pastTheHighestAuthoredTierIsRefusedNotExtrapolated() async {
        let start = room(itemCount: 18, tier: 3)
        let (addition, ratchet, _, store) = makeAddition(room: start)

        let result = await addition.add(
            catalogModelID: "model", to: start, table: table, accessToken: "t"
        )

        guard case .failure(.expansionFailed(.capacityExhausted(let count, let highest))) = result else {
            return XCTFail("expected capacityExhausted, got \(result)")
        }
        XCTAssertEqual(count, 19)
        XCTAssertEqual(highest, CollectionTier(3))
        XCTAssertTrue(ratchet.requests.isEmpty)
        XCTAssertTrue(store.addCalls.isEmpty)
    }
}
