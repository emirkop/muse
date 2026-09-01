import XCTest
@testable import MuseApp

final class RoomPhotoOrderTests: XCTestCase {

    private func slots(_ count: Int) -> [PhotoSlotAssignment] {
        (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: "caption \($0)") }
    }

    private func ids(_ slots: [PhotoSlotAssignment]) -> [String] {
        RoomPhotoOrder.normalised(slots).map(\.photoAssetID)
    }

    // MARK: - Swap mechanics

    func test_twoPhotoSwap() {
        let result = RoomPhotoOrder.swapping(slots(2), from: 0, to: 1)
        XCTAssertEqual(ids(result), ["asset_1", "asset_0"])
        XCTAssertEqual(result.map(\.slotIndex), [0, 1])
    }

    func test_swapExchangesOnlyTheTwo_neverCascades() {
        let result = RoomPhotoOrder.swapping(slots(6), from: 1, to: 4)
        XCTAssertEqual(ids(result), ["asset_0", "asset_4", "asset_2", "asset_3", "asset_1", "asset_5"])
    }

    func test_swapIsSymmetric() {
        let forward = RoomPhotoOrder.swapping(slots(5), from: 1, to: 3)
        let backward = RoomPhotoOrder.swapping(slots(5), from: 3, to: 1)
        XCTAssertEqual(ids(forward), ids(backward))
    }

    func test_swappingTwiceReturnsTheOriginal() {
        let once = RoomPhotoOrder.swapping(slots(7), from: 2, to: 5)
        let twice = RoomPhotoOrder.swapping(once, from: 2, to: 5)
        XCTAssertEqual(ids(twice), ids(slots(7)))
    }

    // MARK: - No-ops

    func test_sourceEqualsDestination_isANoOp() {
        for position in 0..<4 {
            let result = RoomPhotoOrder.swapping(slots(4), from: position, to: position)
            XCTAssertEqual(ids(result), ids(slots(4)), "position \(position)")
        }
    }

    func test_outOfRangePositions_areNoOps() {
        for (from, to) in [(0, 9), (9, 0), (-1, 1), (1, -1), (5, 5)] {
            let result = RoomPhotoOrder.swapping(slots(4), from: from, to: to)
            XCTAssertEqual(ids(result), ids(slots(4)), "(\(from), \(to))")
        }
    }

    func test_singlePhotoRoom_cannotBeReordered() {
        let result = RoomPhotoOrder.swapping(slots(1), from: 0, to: 0)
        XCTAssertEqual(ids(result), ["asset_0"])
    }

    func test_emptyRoom_isSafe() {
        XCTAssertTrue(RoomPhotoOrder.swapping([], from: 0, to: 1).isEmpty)
    }

    // MARK: - Invariants at every count

    func test_everySwapIn28PhotoRoom_preservesCountIdentityAndCaptions() {
        let original = slots(Room.maxPhotos)
        let captionFor = Dictionary(uniqueKeysWithValues: original.map { ($0.photoAssetID, $0.caption) })

        for from in 0..<Room.maxPhotos {
            for to in 0..<Room.maxPhotos {
                let result = RoomPhotoOrder.swapping(original, from: from, to: to)

                XCTAssertEqual(result.count, Room.maxPhotos, "(\(from),\(to)) count")
                XCTAssertEqual(Set(result.map(\.photoAssetID)).count, Room.maxPhotos, "(\(from),\(to)) no duplicates")
                XCTAssertEqual(Set(result.map(\.photoAssetID)), Set(original.map(\.photoAssetID)), "(\(from),\(to)) no missing")
                XCTAssertEqual(result.map(\.slotIndex), Array(0..<Room.maxPhotos), "(\(from),\(to)) contiguous")
                for slot in result {
                    XCTAssertEqual(slot.caption, captionFor[slot.photoAssetID], "(\(from),\(to)) caption stayed with its photograph")
                }
            }
        }
    }

    func test_isDeterministic() {
        for _ in 0..<5 {
            XCTAssertEqual(
                ids(RoomPhotoOrder.swapping(slots(9), from: 2, to: 7)),
                ids(RoomPhotoOrder.swapping(slots(9), from: 2, to: 7))
            )
        }
    }

    // MARK: - Normalisation and helpers

    func test_normalisation_readsSlotIndexNotArrayOrder() {
        let shuffled = slots(4).reversed().map { $0 }
        XCTAssertEqual(ids(shuffled), ["asset_0", "asset_1", "asset_2", "asset_3"])

        let result = RoomPhotoOrder.swapping(shuffled, from: 0, to: 3)
        XCTAssertEqual(ids(result), ["asset_3", "asset_1", "asset_2", "asset_0"])
    }

    func test_normalisation_reindexesGapsContiguously() {
        let gapped = [
            PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a", caption: ""),
            PhotoSlotAssignment(slotIndex: 5, photoAssetID: "b", caption: ""),
            PhotoSlotAssignment(slotIndex: 9, photoAssetID: "c", caption: "")
        ]
        let result = RoomPhotoOrder.normalised(gapped)
        XCTAssertEqual(result.map(\.slotIndex), [0, 1, 2])
        XCTAssertEqual(result.map(\.photoAssetID), ["a", "b", "c"])
    }

    func test_assetIDs_isTheRequestBodyOrder() {
        let swapped = RoomPhotoOrder.swapping(slots(3), from: 0, to: 2)
        XCTAssertEqual(RoomPhotoOrder.assetIDs(swapped), ["asset_2", "asset_1", "asset_0"])
    }

    func test_position_ofAsset() {
        XCTAssertEqual(RoomPhotoOrder.position(ofAssetID: "asset_3", in: slots(5)), 3)
        XCTAssertNil(RoomPhotoOrder.position(ofAssetID: "nope", in: slots(5)))
    }

    // MARK: - Room copy

    func test_replacingPhotoSlots_carriesEverythingElseThrough() {
        let room = Room(
            id: "r1", name: "Trabzon", variantID: "v1", privacy: .public,
            photoSlots: slots(2),
            sculptures: [SculptureInstance(slotIndex: 0, catalogID: "sculpture_a")]
        )
        let reordered = room.replacingPhotoSlots(RoomPhotoOrder.swapping(room.photoSlots, from: 0, to: 1))

        XCTAssertEqual(reordered.id, room.id)
        XCTAssertEqual(reordered.name, room.name)
        XCTAssertEqual(reordered.variantID, room.variantID)
        XCTAssertEqual(reordered.privacy, room.privacy)
        XCTAssertEqual(reordered.sculptures, room.sculptures)
        XCTAssertEqual(ids(reordered.photoSlots), ["asset_1", "asset_0"])
    }

    // MARK: - Against placement engine

    func test_swapChangesOccupants_notTheAnchorSet() {
        let table = PlaceholderRoomSlotTable.build()
        for count in [2, 5, 14, 27, Room.maxPhotos] {
            let room = Room(id: "r", name: "n", variantID: table.variantID, privacy: .private, photoSlots: slots(count))
            guard case .resolved(let before) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
                return XCTFail("count \(count) must resolve")
            }
            let swapped = room.replacingPhotoSlots(RoomPhotoOrder.swapping(room.photoSlots, from: 0, to: count - 1))
            guard case .resolved(let after) = RoomPlacementResolver.resolve(room: swapped, slotTable: table) else {
                return XCTFail("count \(count) must still resolve")
            }

            XCTAssertEqual(before.map(\.anchor), after.map(\.anchor), "count \(count): the anchor sequence is unchanged")
            XCTAssertEqual(after.first?.photoAssetID, before.last?.photoAssetID, "count \(count): the photographs exchanged")
            XCTAssertEqual(after.last?.photoAssetID, before.first?.photoAssetID)
            for placement in after {
                XCTAssertEqual(placement.caption, "caption \(placement.photoAssetID.dropFirst("asset_".count))")
            }
        }
    }

    func test_focalAndRearSwap_adoptsDestinationAnchors() {
        let table = PlaceholderRoomSlotTable.build()
        let room = Room(id: "r", name: "n", variantID: table.variantID, privacy: .private, photoSlots: slots(Room.maxPhotos))
        let swapped = room.replacingPhotoSlots(RoomPhotoOrder.swapping(room.photoSlots, from: 0, to: 27))

        guard case .resolved(let after) = RoomPlacementResolver.resolve(room: swapped, slotTable: table) else {
            return XCTFail("must resolve")
        }
        XCTAssertEqual(after[0].anchor.wall, .focal)
        XCTAssertEqual(after[0].photoAssetID, "asset_27", "the rear photograph now hangs on the focal wall")
        XCTAssertEqual(after[27].anchor.wall, .rear)
        XCTAssertEqual(after[27].photoAssetID, "asset_0", "the focal photograph now hangs on the rear wall")
    }

    func test_sameWallSwap_staysOnThatWall() {
        let table = PlaceholderRoomSlotTable.build()
        let room = Room(id: "r", name: "n", variantID: table.variantID, privacy: .private, photoSlots: slots(Room.maxPhotos))
        let layout = RoomPhotoSlotLayout.slots(forPhotoCount: Room.maxPhotos)
        XCTAssertEqual(layout[1].wall, .left)
        XCTAssertEqual(layout[3].wall, .left)

        let swapped = room.replacingPhotoSlots(RoomPhotoOrder.swapping(room.photoSlots, from: 1, to: 3))
        guard case .resolved(let after) = RoomPlacementResolver.resolve(room: swapped, slotTable: table) else {
            return XCTFail("must resolve")
        }
        XCTAssertEqual(after[1].anchor, layout[1].anchor)
        XCTAssertEqual(after[3].anchor, layout[3].anchor)
        XCTAssertEqual(after[1].photoAssetID, "asset_3")
        XCTAssertEqual(after[3].photoAssetID, "asset_1")
    }
}
