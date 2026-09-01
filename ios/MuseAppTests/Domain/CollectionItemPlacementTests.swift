import XCTest
import simd
@testable import MuseApp

final class CollectionItemPlacementTests: XCTestCase {

    private let table = fixtureTierTable

    private func items(_ slots: [Int]) -> [CollectionItem] {
        slots.enumerated().map { position, slot in
            CollectionItem(id: "item-\(position)", slotIndex: slot, catalogModelID: "model")
        }
    }

    private func available(_ tier: Int) -> Set<Int> {
        table.availableSlotIndices(atTier: CollectionTier(tier))
    }

    private var authored: Set<Int> { table.authoredSlotIndices }

    // MARK: - The three write outcomes ( rule 4)

    func test_occupiedTargetSwaps() {
        let room = items([0, 1, 2])

        let drop = CollectionItemPlacement.resolveDrop(
            items: room, movingItemID: "item-0", toSlot: 2,
            availableSlotIndices: available(1), authoredSlotIndices: authored
        )

        XCTAssertEqual(drop, .swap(with: room[2]))
    }

    func test_emptyAvailableTargetMoves() {
        let room = items([0, 1, 2])

        let drop = CollectionItemPlacement.resolveDrop(
            items: room, movingItemID: "item-1", toSlot: 3,
            availableSlotIndices: available(1), authoredSlotIndices: authored
        )

        XCTAssertEqual(drop, .move(toSlot: 3))
    }

    func test_theSameSlotIsANoOp() {
        let room = items([0, 1])

        let drop = CollectionItemPlacement.resolveDrop(
            items: room, movingItemID: "item-1", toSlot: 1,
            availableSlotIndices: available(1), authoredSlotIndices: authored
        )

        XCTAssertEqual(drop, .noChange)
        XCTAssertEqual(
            CollectionItemPlacement.applying(drop, to: room, movingItemID: "item-1"),
            room
        )
    }

    // MARK: - rule 4's rejection, and rule 3's guarantee

    func test_aSlotBelongingToAnUnreachedTierIsRejected() {
        let room = items([0, 1, 2])

        for futureSlot in [4, 9, 10, 17] {
            let drop = CollectionItemPlacement.resolveDrop(
                items: room, movingItemID: "item-0", toSlot: futureSlot,
                availableSlotIndices: available(1), authoredSlotIndices: authored
            )
            XCTAssertEqual(drop, .rejected(.tierNotReached), "slot \(futureSlot) at tier 1")
            XCTAssertEqual(
                CollectionItemPlacement.applying(drop, to: room, movingItemID: "item-0"),
                room
            )
        }
    }

    func test_aSlotTheDesignDoesNotAuthorIsRejectedForItsOwnReason() {
        let room = items([0, 1])

        let drop = CollectionItemPlacement.resolveDrop(
            items: room, movingItemID: "item-0", toSlot: 18,
            availableSlotIndices: available(3), authoredSlotIndices: authored
        )
        XCTAssertEqual(drop, .rejected(.slotNotAuthored))
    }

    func test_anUnknownItemIsRejected() {
        let drop = CollectionItemPlacement.resolveDrop(
            items: items([0, 1]), movingItemID: "not-here", toSlot: 1,
            availableSlotIndices: available(1), authoredSlotIndices: authored
        )
        XCTAssertEqual(drop, .rejected(.itemNotInRoom))
    }

    // MARK: - rule 2: cross-tier, between reached tiers

    func test_crossTierSwapIsAllowedBetweenReachedTiers() {
        let room = items([0, 1, 2, 3, 4, 5])

        let drop = CollectionItemPlacement.resolveDrop(
            items: room, movingItemID: "item-1", toSlot: 5,
            availableSlotIndices: available(2), authoredSlotIndices: authored
        )
        XCTAssertEqual(drop, .swap(with: room[5]))

        let after = CollectionItemPlacement.applying(drop, to: room, movingItemID: "item-1")
        XCTAssertEqual(after.first { $0.id == "item-1" }?.slotIndex, 5)
        XCTAssertEqual(after.first { $0.id == "item-5" }?.slotIndex, 1)
    }

    func test_crossTierMoveIntoAnEmptyReachedSlotIsAllowed() {
        let room = items([0, 1, 2])

        let drop = CollectionItemPlacement.resolveDrop(
            items: room, movingItemID: "item-0", toSlot: 7,
            availableSlotIndices: available(2), authoredSlotIndices: authored
        )
        XCTAssertEqual(drop, .move(toSlot: 7))

        let after = CollectionItemPlacement.applying(drop, to: room, movingItemID: "item-0")
        XCTAssertEqual(after.first { $0.id == "item-0" }?.slotIndex, 7)
        XCTAssertFalse(after.contains { $0.slotIndex == 0 })
    }

    // MARK: - No shifting cascade

    func test_atMostTwoItemsEverMove() {
        let room = items([0, 1, 2, 3, 4, 5, 6, 7])

        for target in [0, 1, 2, 3, 4, 5, 6, 7, 8, 9] {
            let drop = CollectionItemPlacement.resolveDrop(
                items: room, movingItemID: "item-3", toSlot: target,
                availableSlotIndices: available(2), authoredSlotIndices: authored
            )
            let after = CollectionItemPlacement.applying(drop, to: room, movingItemID: "item-3")

            let moved = zip(
                room.sorted { $0.id < $1.id }, after.sorted { $0.id < $1.id }
            ).filter { $0.0.slotIndex != $0.1.slotIndex }
            XCTAssertLessThanOrEqual(moved.count, 2, "dropping onto slot \(target) moved \(moved.count) items")

            XCTAssertEqual(after.count, room.count)
            XCTAssertEqual(Set(after.map(\.id)), Set(room.map(\.id)))
            XCTAssertEqual(
                Dictionary(uniqueKeysWithValues: after.map { ($0.id, $0.catalogModelID) }),
                Dictionary(uniqueKeysWithValues: room.map { ($0.id, $0.catalogModelID) })
            )
            XCTAssertEqual(Set(after.map(\.slotIndex)).count, after.count)
        }
    }

    // MARK: - Placement: the lowest free reached slot

    func test_lowestFreeSlotFillsHolesBeforeNewSlots() {
        XCTAssertEqual(
            CollectionItemPlacement.lowestFreeSlot(items: items([]), availableSlotIndices: available(1)),
            0
        )
        XCTAssertEqual(
            CollectionItemPlacement.lowestFreeSlot(items: items([0, 1]), availableSlotIndices: available(1)),
            2
        )
        XCTAssertEqual(
            CollectionItemPlacement.lowestFreeSlot(items: items([1, 2, 3]), availableSlotIndices: available(1)),
            0
        )
        XCTAssertEqual(
            CollectionItemPlacement.lowestFreeSlot(items: items([0, 2, 3]), availableSlotIndices: available(1)),
            1
        )
    }

    func test_aFullReachedTierHasNoFreeSlot() {
        XCTAssertNil(
            CollectionItemPlacement.lowestFreeSlot(
                items: items([0, 1, 2, 3]), availableSlotIndices: available(1)
            )
        )
        XCTAssertEqual(
            CollectionItemPlacement.lowestFreeSlot(
                items: items([0, 1, 2, 3]), availableSlotIndices: available(2)
            ),
            4
        )
    }

    // MARK: - The bulk resolution path (recorded finding)

    func test_bulkResolutionPlacesOnlyReachedSlots() {
        let room = items([0, 2, 5, 12])

        let atTierOne = table.resolvePlacements(for: room, atTier: CollectionTier(1))
        XCTAssertEqual(atTierOne.placed.map(\.item.slotIndex), [0, 2])
        XCTAssertEqual(atTierOne.unresolvable.map(\.slotIndex), [5, 12])

        let atTierTwo = table.resolvePlacements(for: room, atTier: CollectionTier(2))
        XCTAssertEqual(atTierTwo.placed.map(\.item.slotIndex), [0, 2, 5])
        XCTAssertEqual(atTierTwo.unresolvable.map(\.slotIndex), [12])

        let atTierThree = table.resolvePlacements(for: room, atTier: CollectionTier(3))
        XCTAssertEqual(atTierThree.placed.map(\.item.slotIndex), [0, 2, 5, 12])
        XCTAssertTrue(atTierThree.unresolvable.isEmpty)
    }

    func test_anItemOnAnUnauthoredSlotIsReportedNotDiscarded() {
        let room = items([0, 4096])

        let resolved = table.resolvePlacements(for: room, atTier: CollectionTier(3))
        XCTAssertEqual(resolved.placed.count, 1)
        XCTAssertEqual(resolved.unresolvable.map(\.slotIndex), [4096])
        XCTAssertEqual(resolved.placed.count + resolved.unresolvable.count, room.count)
    }

    func test_resolutionIsOrderedBySlotWhateverTheInputOrder() {
        let scrambled = [
            CollectionItem(id: "c", slotIndex: 3, catalogModelID: "m"),
            CollectionItem(id: "a", slotIndex: 0, catalogModelID: "m"),
            CollectionItem(id: "b", slotIndex: 1, catalogModelID: "m")
        ]
        let resolved = table.resolvePlacements(for: scrambled, atTier: CollectionTier(1))
        XCTAssertEqual(resolved.placed.map(\.item.id), ["a", "b", "c"])
        XCTAssertEqual(resolved.placed.map(\.slot.slotIndex), [0, 1, 3])
    }

    // MARK: - The tier is untouchable from here

    func test_replacingItemsCarriesEverythingElseThrough() {
        let room = CollectionRoom(
            id: "c1", name: "Watches", categoryID: "category_watches",
            designID: "design-1", currentTier: CollectionTier(3), items: items([0, 1])
        )

        let rearranged = room.replacingItems(items([1, 0]))

        XCTAssertEqual(rearranged.currentTier, CollectionTier(3))
        XCTAssertEqual(rearranged.id, room.id)
        XCTAssertEqual(rearranged.name, room.name)
        XCTAssertEqual(rearranged.categoryID, room.categoryID)
        XCTAssertEqual(rearranged.designID, room.designID)
    }

    // MARK: - Independence from the Museum's reorder types

    func test_collectionPlacementDoesNotRenumberTheWayPhotoOrderDoes() {
        let photos = [
            PhotoSlotAssignment(slotIndex: 0, photoAssetID: "p0", caption: ""),
            PhotoSlotAssignment(slotIndex: 1, photoAssetID: "p1", caption: ""),
            PhotoSlotAssignment(slotIndex: 2, photoAssetID: "p2", caption: "")
        ]
        let reordered = RoomPhotoOrder.swapping(photos, from: 0, to: 2)
        XCTAssertEqual(reordered.map(\.slotIndex), [0, 1, 2], "photo slots are always contiguous")

        let room = items([0, 1, 2])
        let moved = CollectionItemPlacement.applying(
            .move(toSlot: 3), to: room, movingItemID: "item-0"
        )
        XCTAssertEqual(moved.map(\.slotIndex), [1, 2, 3], "collection item slots are sparse by decision")
    }
}
