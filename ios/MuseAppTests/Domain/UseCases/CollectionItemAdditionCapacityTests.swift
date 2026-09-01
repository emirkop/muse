import XCTest
@testable import MuseApp

@MainActor
final class CollectionItemAdditionCapacityTests: XCTestCase {
    private let table = fixtureTierTable
    private let model = CollectionCatalogModel(id: "model-1", brandID: "brand", brandDisplayName: "Brand", categoryID: "category_watches", displayName: "Model")

    private func room(items: Int) -> CollectionRoom {
        CollectionRoom(
            id: "cr-1", name: "Watches", categoryID: "category_watches", designID: "design_dev",
            currentTier: .base,
            items: (0..<items).map { CollectionItem(id: "i\($0)", slotIndex: $0, catalogModelID: "m") }
        )
    }

    private func addition(_ items: FakeCollectionItemStore) -> CollectionItemAddition {
        CollectionItemAddition(
            expansion: CollectionTierExpansion(ratchet: FakeTierRatchet(), geometry: FakeTierGeometry()),
            items: items
        )
    }

    func test_serverCapacityRefusal_isItsOwnFailure() async {
        let store = FakeCollectionItemStore(room: room(items: 2))
        store.addError = CollectionAPIError(statusCode: 402, code: "item_capacity_reached", message: "capacity")

        let result = await addition(store).add(catalogModelID: "model-1", to: room(items: 2), table: table, accessToken: "t")

        XCTAssertEqual(result, .failure(.capacityReached))
    }

    func test_aFullDesign_isNotACapacityRefusal() async {
        let store = FakeCollectionItemStore(room: room(items: 18))
        let result = await addition(store).add(catalogModelID: "model-1", to: room(items: 18), table: table, accessToken: "t")
        guard case .failure(.expansionFailed(.capacityExhausted)) = result else {
            return XCTFail("a full Design is an expansion failure, got \(result)")
        }
    }

    func test_viewModel_entersTheCapacityReachedState() async {
        let store = FakeCollectionItemStore(room: room(items: 2))
        store.addError = CollectionAPIError(statusCode: 402, code: "item_capacity_reached", message: "capacity")
        let viewModel = CollectionItemAdditionViewModel(
            model: model, room: room(items: 2), addition: addition(store),
            tables: FakeCollectionDesignTables(table: table), accessToken: "t"
        )

        await viewModel.confirm()

        XCTAssertEqual(viewModel.state, .capacityReached)
    }

    func test_confirmationScreen_offersUpgradeOnCapacityReached() async {
        let store = FakeCollectionItemStore(room: room(items: 2))
        store.addError = CollectionAPIError(statusCode: 402, code: "item_capacity_reached", message: "capacity")
        let viewModel = CollectionItemAdditionViewModel(
            model: model, room: room(items: 2), addition: addition(store),
            tables: FakeCollectionDesignTables(table: table), accessToken: "t"
        )
        let controller = CollectionItemConfirmationViewController(viewModel: viewModel, roomName: "Watches", onAdded: { _ in })
        var upgradeRequested = 0
        controller.onCapacityReached = { upgradeRequested += 1 }
        controller.loadViewIfNeeded()

        await viewModel.confirm()

        XCTAssertEqual(controller.testAddTitle, "See Upgrade Options")
        XCTAssertTrue(controller.testAddEnabled)
        XCTAssertEqual(controller.testOutcomeText, "You've reached your collection's item capacity. The item wasn't added.")
        controller.testTapAdd()
        XCTAssertEqual(upgradeRequested, 1)
        XCTAssertEqual(store.addCalls.count, 1, "the tap must not retry the add")
    }

    func test_isAtCapacity_isTheServersNumbers() {
        XCTAssertTrue(AccountEntitlement(state: .free, itemCapacity: 3, itemCount: 3).isAtCapacity)
        XCTAssertFalse(AccountEntitlement(state: .free, itemCapacity: 3, itemCount: 2).isAtCapacity)
        XCTAssertTrue(AccountEntitlement(state: .revoked, itemCapacity: 3, itemCount: 5).isAtCapacity)
        XCTAssertFalse(AccountEntitlement(state: .paid, itemCapacity: 6, itemCount: 5).isAtCapacity)
    }
}
