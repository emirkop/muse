import XCTest
@testable import MuseApp

@MainActor
final class CollectionItemEditCoordinatorTests: XCTestCase {

    private let table = fixtureTierTable

    private func room(slots: [Int], tier: Int = 1) -> CollectionRoom {
        CollectionRoom(
            id: "c1", name: "Watches",
            categoryID: "category_watches", designID: "dev-fixture:collection-design",
            currentTier: CollectionTier(tier),
            items: slots.map { CollectionItem(id: "item-\($0)", slotIndex: $0, catalogModelID: "model") }
        )
    }

    private func makeCoordinator(
        room start: CollectionRoom
    ) -> (CollectionItemEditCoordinator, FakeCollectionItemStore, FakeCollectionService) {
        let store = FakeCollectionItemStore(room: start)
        let rooms = FakeCollectionService()
        rooms.seed(start)
        let coordinator = CollectionItemEditCoordinator(
            room: start, table: table, items: store, rooms: rooms, accessToken: "t"
        )
        return (coordinator, store, rooms)
    }

    private func slotOf(_ coordinator: CollectionItemEditCoordinator, _ itemID: String) -> Int? {
        coordinator.room.items.first { $0.id == itemID }?.slotIndex
    }

    private func settle() async {
        await Task.yield()
        try? await Task.sleep(for: .milliseconds(30))
    }

    // MARK: - Verify 5: an occupied target swaps

    func test_droppingOnAnOccupiedSlotSwapsOptimisticallyThenConfirms() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1, 2]))

        coordinator.drop(itemID: "item-0", onSlot: 2)

        XCTAssertEqual(coordinator.status, .saving)
        XCTAssertEqual(slotOf(coordinator, "item-0"), 2)
        XCTAssertEqual(slotOf(coordinator, "item-2"), 0)
        XCTAssertEqual(slotOf(coordinator, "item-1"), 1, "the uninvolved item must not move")

        await settle()

        XCTAssertEqual(coordinator.status, .saved)
        XCTAssertEqual(store.placeCalls, [
            .init(collectionRoomID: "c1", collectionItemID: "item-0", slotIndex: 2)
        ])
        XCTAssertEqual(slotOf(coordinator, "item-0"), 2)
        XCTAssertEqual(slotOf(coordinator, "item-2"), 0)
    }

    // MARK: - Verify 6: an empty target moves, nothing else shifts

    func test_droppingOnAnEmptyAvailableSlotMovesWithoutShifting() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1, 2]))

        coordinator.drop(itemID: "item-1", onSlot: 3)
        await settle()

        XCTAssertEqual(coordinator.status, .saved)
        XCTAssertEqual(slotOf(coordinator, "item-1"), 3)
        XCTAssertEqual(slotOf(coordinator, "item-0"), 0)
        XCTAssertEqual(slotOf(coordinator, "item-2"), 2)
        XCTAssertFalse(coordinator.room.items.contains { $0.slotIndex == 1 })
        XCTAssertEqual(store.placeCalls.count, 1)
    }

    // MARK: - Verify 7: cross-tier swap between reached tiers

    func test_crossTierSwapWorksWhenBothTiersAreReached() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1, 2, 3, 4, 5], tier: 2))

        coordinator.drop(itemID: "item-1", onSlot: 5)
        await settle()

        XCTAssertEqual(coordinator.status, .saved)
        XCTAssertEqual(slotOf(coordinator, "item-1"), 5)
        XCTAssertEqual(slotOf(coordinator, "item-5"), 1)
        XCTAssertEqual(store.placeCalls.count, 1)
        XCTAssertEqual(coordinator.room.currentTier, CollectionTier(2))
    }

    // MARK: - Verify 8: a future-tier drop cannot expand the Room

    func test_aFutureTierDropIsRefusedAndSendsNothing() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1, 2]))
        let before = coordinator.room

        coordinator.drop(itemID: "item-0", onSlot: 7)
        await settle()

        XCTAssertEqual(coordinator.status, .rejected(.tierNotReached))
        XCTAssertTrue(store.placeCalls.isEmpty, "a rejected drop must not reach the server")
        XCTAssertEqual(coordinator.room, before, "a rejected drop must change nothing")
        XCTAssertEqual(coordinator.room.currentTier, before.currentTier)
    }

    func test_aSlotTheDesignDoesNotAuthorIsRefusedWithItsOwnReason() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1], tier: 3))

        coordinator.drop(itemID: "item-0", onSlot: 999)
        await settle()

        XCTAssertEqual(coordinator.status, .rejected(.slotNotAuthored))
        XCTAssertTrue(store.placeCalls.isEmpty)
    }

    func test_droppingBackOnTheSourceIsANoOpAndSendsNothing() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1]))

        coordinator.drop(itemID: "item-1", onSlot: 1)
        await settle()

        XCTAssertEqual(coordinator.status, .idle)
        XCTAssertTrue(store.placeCalls.isEmpty)
    }

    // MARK: - Rollback is deterministic

    func test_aFailedPersistRestoresTheLastConfirmedArrangementExactly() async {
        let start = room(slots: [0, 1, 2])
        let (coordinator, store, _) = makeCoordinator(room: start)
        store.placeError = CollectionAPIError(statusCode: 500, code: nil, message: nil)

        coordinator.drop(itemID: "item-0", onSlot: 2)
        XCTAssertEqual(slotOf(coordinator, "item-0"), 2, "optimistic apply")

        await settle()

        XCTAssertEqual(coordinator.status, .failedTransport)
        XCTAssertEqual(coordinator.room.items, start.items, "rollback must restore the confirmed arrangement")
    }

    func test_repeatedFailuresAlwaysRestoreTheSameArrangement() async {
        let start = room(slots: [0, 1, 2, 3])
        let (coordinator, store, _) = makeCoordinator(room: start)
        store.placeError = CollectionAPIError(statusCode: 503, code: nil, message: nil)

        for target in [3, 1, 2] {
            coordinator.drop(itemID: "item-0", onSlot: target)
            await settle()
            XCTAssertEqual(coordinator.room.items, start.items, "restore after dropping on \(target)")
        }
    }

    func test_aStaleRefusalRollsBackAndReloadsTheAuthoritativeRoom() async {
        let start = room(slots: [0, 1, 2])
        let (coordinator, store, rooms) = makeCoordinator(room: start)
        store.placeError = CollectionAPIError(statusCode: 409, code: "slot_taken", message: nil)
        rooms.rooms = [room(slots: [0, 1, 3])]

        coordinator.drop(itemID: "item-0", onSlot: 2)
        await settle()

        XCTAssertEqual(coordinator.status, .failedStale)
        XCTAssertEqual(coordinator.room.items.map(\.slotIndex), [0, 1, 3], "the reload should have been adopted")
    }

    func test_aClientErrorFailsWithoutRetryingAndWithoutReloading() async {
        let start = room(slots: [0, 1])
        let (coordinator, store, rooms) = makeCoordinator(room: start)
        store.placeError = CollectionAPIError(statusCode: 400, code: "invalid_slot_index", message: nil)

        coordinator.drop(itemID: "item-0", onSlot: 1)
        await settle()

        XCTAssertEqual(coordinator.status, .failedInvalid)
        XCTAssertEqual(coordinator.room.items, start.items)
        XCTAssertEqual(store.placeCalls.count, 1, "nothing may be retried")
        XCTAssertTrue(rooms.fetchedIDs.isEmpty, "a client bug is not a reason to reload")
    }

    // MARK: - Serialization is what makes rollback deterministic

    func test_asecondDropWhileOneIsInFlightIsRefusedNotQueued() async {
        let start = room(slots: [0, 1, 2])
        let (coordinator, store, _) = makeCoordinator(room: start)

        let released = expectation(description: "first write released")
        var release: (() -> Void)?
        store.gate = {
            await withCheckedContinuation { continuation in
                release = { continuation.resume() }
                released.fulfill()
            }
        }

        coordinator.drop(itemID: "item-0", onSlot: 2)
        await fulfillment(of: [released], timeout: 1)

        coordinator.drop(itemID: "item-1", onSlot: 0)
        XCTAssertEqual(coordinator.status, .busy)
        XCTAssertEqual(store.placeCalls.count, 1, "the second drop must not be sent")

        release?()
        await settle()
        XCTAssertEqual(coordinator.status, .saved)
        XCTAssertEqual(store.placeCalls.count, 1)
    }

    // MARK: - Placements and unrenderable items

    func test_placementsAreEmittedForReachedSlotsAndUnresolvableItemsAreReported() async {
        let start = CollectionRoom(
            id: "c1", name: "Watches",
            categoryID: "category_watches", designID: "dev-fixture:collection-design",
            currentTier: .base,
            items: [
                CollectionItem(id: "item-0", slotIndex: 0, catalogModelID: "model"),
                CollectionItem(id: "item-12", slotIndex: 12, catalogModelID: "model")
            ]
        )
        let (coordinator, _, _) = makeCoordinator(room: start)

        XCTAssertEqual(coordinator.placements.map(\.item.id), ["item-0"])
        XCTAssertEqual(coordinator.unresolvableItems.map(\.id), ["item-12"])
    }

    func test_availableSlotsComeFromTheReachedTierOnly() {
        let (atTierOne, _, _) = makeCoordinator(room: room(slots: [0]))
        XCTAssertEqual(atTierOne.availableSlotIndices, Set(0..<4))

        let (atTierTwo, _, _) = makeCoordinator(room: room(slots: [0], tier: 2))
        XCTAssertEqual(atTierTwo.availableSlotIndices, Set(0..<10))

        XCTAssertEqual(atTierOne.authoredSlotIndices, Set(0..<18))
    }

    // MARK: - Adoption

    func test_adoptingAServerRoomMovesTheRollbackBaseline() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1]))

        XCTAssertTrue(coordinator.adopt(room(slots: [0, 1, 2])))
        store.room = room(slots: [0, 1, 2])
        store.placeError = CollectionAPIError(statusCode: 500, code: nil, message: nil)

        coordinator.drop(itemID: "item-0", onSlot: 2)
        await settle()

        XCTAssertEqual(coordinator.room.items.map(\.slotIndex), [0, 1, 2])
    }

    func test_adoptingIsRefusedWhileADropIsUnresolved() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1, 2]))

        let released = expectation(description: "write released")
        var release: (() -> Void)?
        store.gate = {
            await withCheckedContinuation { continuation in
                release = { continuation.resume() }
                released.fulfill()
            }
        }
        coordinator.drop(itemID: "item-0", onSlot: 2)
        await fulfillment(of: [released], timeout: 1)

        XCTAssertFalse(
            coordinator.adopt(room(slots: [0, 1, 2, 3])),
            "adopting mid-flight would replace the arrangement the drop is layered on"
        )

        release?()
        await settle()
    }

    func test_deactivationStopsFurtherDrops() async {
        let (coordinator, store, _) = makeCoordinator(room: room(slots: [0, 1]))
        coordinator.deactivate()

        coordinator.drop(itemID: "item-0", onSlot: 1)
        await settle()

        XCTAssertTrue(store.placeCalls.isEmpty)
    }
}
