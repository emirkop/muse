import XCTest
@testable import MuseApp

final class RoomSculpturesTests: XCTestCase {

    private func placed(_ slots: [Int: String]) -> [SculptureInstance] {
        RoomSculptures.sorted(slots.map { SculptureInstance(slotIndex: $0.key, catalogID: $0.value) })
    }

    // MARK: - Slot layout

    func test_lowestFreeSlot_walksUpFromZero() {
        XCTAssertEqual(RoomSculptureSlotLayout.lowestFreeSlot(occupied: []), 0)
        XCTAssertEqual(RoomSculptureSlotLayout.lowestFreeSlot(occupied: placed([0: "a"])), 1)
        XCTAssertEqual(RoomSculptureSlotLayout.lowestFreeSlot(occupied: placed([0: "a", 1: "b"])), 2)
        XCTAssertNil(RoomSculptureSlotLayout.lowestFreeSlot(occupied: placed([0: "a", 1: "b", 2: "c"])), "a full Room has no free slot")
    }

    func test_lowestFreeSlot_reusesAGapBeforeAHigherSlot() {
        XCTAssertEqual(RoomSculptureSlotLayout.lowestFreeSlot(occupied: placed([1: "b"])), 0)
        XCTAssertEqual(RoomSculptureSlotLayout.lowestFreeSlot(occupied: placed([0: "a", 2: "c"])), 1)
        XCTAssertEqual(RoomSculptureSlotLayout.lowestFreeSlot(occupied: placed([1: "b", 2: "c"])), 0)
    }

    func test_slotValidity_matchesTheConfirmedCap() {
        XCTAssertEqual(RoomSculptureSlotLayout.slotCount, Room.maxSculptures)
        XCTAssertTrue(RoomSculptureSlotLayout.isValid(slotIndex: 0))
        XCTAssertTrue(RoomSculptureSlotLayout.isValid(slotIndex: Room.maxSculptures - 1))
        XCTAssertFalse(RoomSculptureSlotLayout.isValid(slotIndex: -1))
        XCTAssertFalse(RoomSculptureSlotLayout.isValid(slotIndex: Room.maxSculptures))
    }

    // MARK: - Adding

    func test_adding_takesTheLowestFreeSlot() {
        let one = RoomSculptures.adding("a", to: [])!
        XCTAssertEqual(one.map(\.slotIndex), [0])
        XCTAssertEqual(one.map(\.catalogID), ["a"])

        let two = RoomSculptures.adding("b", to: one)!
        XCTAssertEqual(two.map(\.slotIndex), [0, 1])
        XCTAssertEqual(two.map(\.catalogID), ["a", "b"])
    }

    func test_adding_reusesAFreedSlot() {
        let withGap = placed([0: "a", 2: "c"])
        let filled = RoomSculptures.adding("new", to: withGap)!

        XCTAssertEqual(filled.map(\.slotIndex), [0, 1, 2])
        XCTAssertEqual(filled.map(\.catalogID), ["a", "new", "c"], "the freed middle slot is what the newcomer takes")
    }

    func test_adding_toAFullRoom_isRefused() {
        XCTAssertNil(RoomSculptures.adding("d", to: placed([0: "a", 1: "b", 2: "c"])))
    }

    func test_adding_theSameSculptureTwice_isAllowed() {
        let once = RoomSculptures.adding("a", to: [])!
        let twice = RoomSculptures.adding("a", to: once)!
        XCTAssertEqual(twice.map(\.catalogID), ["a", "a"])
        XCTAssertEqual(twice.map(\.slotIndex), [0, 1])
    }

    // MARK: - Removing

    func test_removing_leavesTheSlotEmpty_andMovesNothingElse() {
        let full = placed([0: "a", 1: "b", 2: "c"])

        let after = RoomSculptures.removing(slotIndex: 1, from: full)

        XCTAssertEqual(after.map(\.slotIndex), [0, 2], "no compaction — indices are not re-packed")
        XCTAssertEqual(after.map(\.catalogID), ["a", "c"])
        XCTAssertFalse(RoomSculptures.isOccupied(slotIndex: 1, in: after))
        XCTAssertTrue(RoomSculptures.isOccupied(slotIndex: 2, in: after))
    }

    func test_removing_first_last_andOnly() {
        let full = placed([0: "a", 1: "b", 2: "c"])
        XCTAssertEqual(RoomSculptures.removing(slotIndex: 0, from: full).map(\.slotIndex), [1, 2])
        XCTAssertEqual(RoomSculptures.removing(slotIndex: 2, from: full).map(\.slotIndex), [0, 1])
        XCTAssertEqual(RoomSculptures.removing(slotIndex: 0, from: placed([0: "a"])), [])
    }

    func test_removing_anEmptySlot_changesNothing() {
        let one = placed([0: "a"])
        XCTAssertEqual(RoomSculptures.removing(slotIndex: 1, from: one), one)
        XCTAssertEqual(RoomSculptures.removing(slotIndex: 99, from: one), one)
    }

    func test_sorted_ordersBySlot_withoutReindexing() {
        let unordered = [
            SculptureInstance(slotIndex: 2, catalogID: "c"),
            SculptureInstance(slotIndex: 0, catalogID: "a")
        ]
        let sorted = RoomSculptures.sorted(unordered)
        XCTAssertEqual(sorted.map(\.slotIndex), [0, 2], "gaps survive sorting")
        XCTAssertEqual(sorted.map(\.catalogID), ["a", "c"])
    }

    // MARK: - Sculptures never touch photographs

    func test_replacingSculptures_carriesPhotographsThroughUntouched() {
        let room = Room(
            id: "r", name: "Trabzon", variantID: "v", privacy: .private,
            photoSlots: (0..<4).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: "caption \($0)") },
            sculptures: placed([0: "a"])
        )

        let changed = room.replacingSculptures(RoomSculptures.removing(slotIndex: 0, from: room.sculptures))

        XCTAssertEqual(changed.photoSlots, room.photoSlots, "photographs, order and captions are untouched")
        XCTAssertEqual(changed.id, room.id)
        XCTAssertEqual(changed.name, room.name)
        XCTAssertEqual(changed.variantID, room.variantID)
        XCTAssertEqual(changed.privacy, room.privacy)
        XCTAssertTrue(changed.sculptures.isEmpty)
    }

    func test_photoReorder_carriesSculpturesThroughUntouched() {
        let sculptures = placed([0: "a", 2: "c"])
        let room = Room(
            id: "r", name: "n", variantID: "v", privacy: .private,
            photoSlots: (0..<3).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: "") },
            sculptures: sculptures
        )

        let reordered = room.replacingPhotoSlots(RoomPhotoOrder.swapping(room.photoSlots, from: 0, to: 2))

        XCTAssertEqual(reordered.sculptures, sculptures)
        XCTAssertEqual(RoomPhotoOrder.assetIDs(reordered.photoSlots), ["asset_2", "asset_1", "asset_0"])
    }

    func test_capacity_matchesTheConfirmedCap() {
        let room = Room(id: "r", name: "n", variantID: "v", privacy: .private, sculptures: placed([0: "a", 1: "b"]))
        XCTAssertTrue(room.hasCapacityForSculpture)
        let full = room.replacingSculptures(placed([0: "a", 1: "b", 2: "c"]))
        XCTAssertFalse(full.hasCapacityForSculpture)
    }
}
