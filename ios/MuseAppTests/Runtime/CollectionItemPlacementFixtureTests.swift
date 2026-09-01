import XCTest
@testable import MuseApp

final class CollectionItemPlacementFixtureTests: XCTestCase {

    func test_tierTable_isAcceptedByTheDomainRule() {
        let table = CollectionItemPlacementFixture.tierTable()
        XCTAssertNil(
            table.rejection,
            "the fixture's own table is refused by CollectionTierTable.rejection"
        )
    }

    func test_eachTierContributesOnlyItsOwnAddedSlots() {
        let tiers = CollectionItemPlacementFixture.tierTable().tiers
        XCTAssertEqual(tiers.count, 3)
        XCTAssertEqual(tiers[0].itemTransforms.count, 4, "tier 1 should contribute 4 slots")
        XCTAssertEqual(tiers[1].itemTransforms.count, 6, "tier 2 should contribute only the next 6")
        XCTAssertEqual(tiers[2].itemTransforms.count, 8, "tier 3 should contribute only the next 8")
    }

    func test_cumulativeCapacitiesAreUnchanged() {
        let tiers = CollectionItemPlacementFixture.tierTable().tiers
        XCTAssertEqual(tiers.map(\.cumulativeCapacity), [4, 10, 18])
        XCTAssertEqual(CollectionItemPlacementFixture.cumulativeCapacities, [4, 10, 18])
    }

    func test_totalSlotCountMatchesTheHighestCumulativeCapacity() {
        let tiers = CollectionItemPlacementFixture.tierTable().tiers
        let total = tiers.reduce(0) { $0 + $1.itemTransforms.count }
        XCTAssertEqual(total, 18, "the fully expanded fixture Room holds 18 slots, not the sum of the cumulative figures")
    }

    func test_slotIndicesAreUniqueAndContiguous() {
        let indices = CollectionItemPlacementFixture.tierTable()
            .tiers
            .flatMap(\.itemTransforms)
            .map(\.slotIndex)
        XCTAssertEqual(Set(indices).count, indices.count, "duplicate slot indices")
        XCTAssertEqual(indices, Array(0..<indices.count), "slot indices are not contiguous from 0")
    }
}
