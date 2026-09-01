import XCTest
@testable import MuseApp

final class CollectionTierTests: XCTestCase {

    // MARK: - The table itself

    func test_malformedTables_areRejected() {
        let cases: [([Int], CollectionTierCapacities.Rejection)] = [
            ([], .empty),
            ([0], .notStrictlyIncreasing),
            ([-4], .notStrictlyIncreasing),
            ([10, 10], .notStrictlyIncreasing),
            ([10, 6], .notStrictlyIncreasing),
            ([10, 20, 15], .notStrictlyIncreasing)
        ]
        for (table, expected) in cases {
            XCTAssertEqual(
                CollectionTierCapacities(table).rejection, expected,
                "table \(table) should be rejected as \(expected)"
            )
        }
    }

    func test_anyStrictlyGrowingTable_isAccepted() {
        for table in [[1], [10, 24, 48], [3, 4, 5, 6, 7], [1, 1_000, 100_000]] {
            XCTAssertNil(
                CollectionTierCapacities(table).rejection,
                "table \(table) is a coherent growth sequence"
            )
        }
    }

    func test_capacityAndHighestTier() {
        let table = CollectionTierCapacities([10, 24, 48])

        XCTAssertEqual(table.highestTier, CollectionTier(3))
        XCTAssertEqual(table.capacity(at: CollectionTier(1)), 10)
        XCTAssertEqual(table.capacity(at: CollectionTier(2)), 24)
        XCTAssertEqual(table.capacity(at: CollectionTier(3)), 48)

        for ordinal in [0, -1, 4, 99] {
            XCTAssertNil(
                table.capacity(at: CollectionTier(ordinal)),
                "tier \(ordinal) is outside the design's authored range"
            )
        }
    }

    // MARK: - The core arithmetic

    func test_requiredTier_atEveryBoundary() {
        let table = CollectionTierCapacities([10, 24, 48])
        let expected: [(Int, Int)] = [
            (0, 1), (1, 1), (9, 1),
            (10, 1),
            (11, 2),
            (23, 2),
            (24, 2),
            (25, 3),
            (47, 3),
            (48, 3)
        ]
        for (itemCount, wantOrdinal) in expected {
            switch table.requiredTier(forItemCount: itemCount) {
            case .success(let tier):
                XCTAssertEqual(
                    tier, CollectionTier(wantOrdinal),
                    "\(itemCount) items should require tier \(wantOrdinal)"
                )
            case .failure(let failure):
                XCTFail("\(itemCount) items failed to resolve: \(failure)")
            }
        }
    }

    func test_requiredTier_pastTheHighestTier_isExhausted() {
        let table = CollectionTierCapacities([10, 24, 48])
        for itemCount in [49, 10_000] {
            XCTAssertEqual(
                table.requiredTier(forItemCount: itemCount),
                .failure(.exhausted),
                "\(itemCount) items exceeds the design's authored tiers"
            )
        }
    }

    func test_requiredTier_rejectsBadInput() {
        XCTAssertEqual(
            CollectionTierCapacities([10]).requiredTier(forItemCount: -1),
            .failure(.negativeItemCount)
        )
        XCTAssertEqual(
            CollectionTierCapacities([10, 5]).requiredTier(forItemCount: 3),
            .failure(.malformedTable(.notStrictlyIncreasing))
        )
    }

    func test_singleTierDesign() {
        let table = CollectionTierCapacities([5])
        for itemCount in 0...5 {
            XCTAssertEqual(
                table.requiredTier(forItemCount: itemCount),
                .success(.base),
                "\(itemCount) items fit the only authored tier"
            )
        }
        XCTAssertEqual(table.requiredTier(forItemCount: 6), .failure(.exhausted))
    }

    func test_requiredTier_isSymmetric_soTheRatchetPolicyStaysOpen() {
        let table = CollectionTierCapacities([10, 24, 48])
        XCTAssertEqual(table.requiredTier(forItemCount: 30), .success(CollectionTier(3)))
        XCTAssertEqual(table.requiredTier(forItemCount: 8), .success(CollectionTier(1)))
    }

    // MARK: - Slots

    func test_collectionRoom_hasNoItemCap() {
        let items = (0..<500).map {
            CollectionItem(id: "i\($0)", slotIndex: $0, catalogModelID: "m")
        }
        let room = CollectionRoom(id: "c1", name: "Big", items: items)

        XCTAssertEqual(room.itemCount, 500)
        XCTAssertEqual(
            CollectionItemPlacement.lowestFreeSlot(
                items: items, availableSlotIndices: Set(0...500)
            ),
            500,
            "no ceiling may exist"
        )
    }

    // MARK: - The content model's defaults

    func test_newCollectionRoom_startsAtTheBaseTierWithNothingChosen() {
        let room = CollectionRoom(id: "c1", name: "Untitled")

        XCTAssertEqual(room.currentTier, .base)
        XCTAssertEqual(room.currentTier.ordinal, 1, "the base tier is 1, not 0")
        XCTAssertTrue(room.items.isEmpty)
        XCTAssertNil(room.categoryID)
        XCTAssertNil(room.designID)
        XCTAssertTrue(room.needsCategory)
        XCTAssertTrue(room.needsDesign)
    }
}

final class CollectionRoomPatchTests: XCTestCase {

    func test_eachConvenienceCarriesExactlyOneField() {
        let name = CollectionRoomPatch.name("Watches")
        XCTAssertEqual(name.name, "Watches")
        XCTAssertNil(name.categoryID)
        XCTAssertNil(name.designID)

        let category = CollectionRoomPatch.category("watches")
        XCTAssertEqual(category.categoryID, "watches")
        XCTAssertNil(category.name)
        XCTAssertNil(category.designID)

        let design = CollectionRoomPatch.design("display-case")
        XCTAssertEqual(design.designID, "display-case")
        XCTAssertNil(design.name)
        XCTAssertNil(design.categoryID)
    }

    func test_emptyPatchIsRecognised() {
        XCTAssertTrue(CollectionRoomPatch().isEmpty)
        XCTAssertFalse(CollectionRoomPatch.name("x").isEmpty)
    }
}

final class CollectionRoomNamingRulesTests: XCTestCase {

    func test_validation() {
        XCTAssertNil(CollectionRoomNamingRules.rejection(for: "Watches"))
        XCTAssertNil(CollectionRoomNamingRules.rejection(for: "Saat Koleksiyonum"))
        XCTAssertNil(CollectionRoomNamingRules.rejection(for: "⌚️ Watches"))

        XCTAssertEqual(CollectionRoomNamingRules.rejection(for: ""), .empty)
        XCTAssertEqual(CollectionRoomNamingRules.rejection(for: "   \n "), .empty)

        let limit = CollectionRoomNamingRules.interimMaximumLength
        XCTAssertNil(CollectionRoomNamingRules.rejection(for: String(repeating: "a", count: limit)))
        XCTAssertEqual(
            CollectionRoomNamingRules.rejection(for: String(repeating: "a", count: limit + 1)),
            .tooLong(limit: limit)
        )
    }

    func test_rulesRemainInterimAndPermissive() {
        XCTAssertFalse(
            CollectionRoomNamingRules.interimEnforcesUniqueness,
            " has not decided uniqueness; `02` says duplicates are allowed"
        )
        XCTAssertFalse(
            CollectionRoomNamingRules.interimAppliesProfanityFilter,
            "a profanity filter would be unowned content policy"
        )
    }

    func test_clientBoundFitsInsideTheServersByteBound() {
        let worstCase = String(repeating: "⌚", count: CollectionRoomNamingRules.interimMaximumLength)
        XCTAssertNil(CollectionRoomNamingRules.rejection(for: worstCase))
        XCTAssertLessThanOrEqual(
            worstCase.utf8.count, 200,
            "a name this client accepts must never exceed the server's 200-byte bound"
        )
    }

    func test_placeholderIsNotAValidSubmission() {
        XCTAssertFalse(CollectionRoomNamingRules.placeholderExample.isEmpty)
        XCTAssertTrue(CollectionRoomNamingRules.placeholderExample.hasPrefix("e.g."))
    }
}
