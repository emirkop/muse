import XCTest
@testable import MuseApp

final class RoomPhotoReplacementTests: XCTestCase {

    private func slots(_ count: Int) -> [PhotoSlotAssignment] {
        (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: "caption \($0)") }
    }

    func test_replacing_swapsTheIdentity_keepingSlotIndexAndCaption() {
        let result = RoomPhotoReplacement.replacing("asset_2", with: "new", in: slots(5))

        XCTAssertEqual(result.map(\.photoAssetID), ["asset_0", "asset_1", "new", "asset_3", "asset_4"])
        XCTAssertEqual(result[2].slotIndex, 2, "position preserved")
        XCTAssertEqual(result[2].caption, "caption 2", "caption preserved")
    }

    func test_replacing_leavesEveryOtherSlotUntouched() {
        let before = slots(6)
        let result = RoomPhotoReplacement.replacing("asset_3", with: "new", in: before)

        for (index, slot) in result.enumerated() where index != 3 {
            XCTAssertEqual(slot, before[index], "slot \(index) must be untouched")
        }
        XCTAssertEqual(result.count, before.count)
    }

    func test_replacing_anUnknownPhotograph_changesNothing() {
        let before = slots(3)
        XCTAssertEqual(RoomPhotoReplacement.replacing("ghost", with: "new", in: before), before)
    }

    func test_replacing_withItself_changesNothing() {
        let before = slots(3)
        XCTAssertEqual(RoomPhotoReplacement.replacing("asset_1", with: "asset_1", in: before), before)
    }

    func test_replacing_worksOnAnUnorderedInput_andPreservesOrdering() {
        let shuffled = [
            PhotoSlotAssignment(slotIndex: 2, photoAssetID: "c", caption: "C"),
            PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a", caption: "A"),
            PhotoSlotAssignment(slotIndex: 1, photoAssetID: "b", caption: "B")
        ]
        let result = RoomPhotoReplacement.replacing("a", with: "new", in: shuffled)

        XCTAssertEqual(RoomPhotoOrder.assetIDs(result), ["new", "b", "c"])
        XCTAssertEqual(RoomPhotoCaptions.caption(forAssetID: "new", in: result), "A")
    }

    func test_replacing_resolvesThroughToTheSameTransforms() {
        let table = PlaceholderRoomSlotTable.build()
        let before = Room(id: "r", name: "n", variantID: table.variantID, privacy: .private, photoSlots: slots(Room.maxPhotos))
        let after = before.replacingPhotoSlots(RoomPhotoReplacement.replacing("asset_13", with: "new", in: before.photoSlots))

        guard case .resolved(let placementsBefore) = RoomPlacementResolver.resolve(room: before, slotTable: table),
              case .resolved(let placementsAfter) = RoomPlacementResolver.resolve(room: after, slotTable: table) else {
            return XCTFail("fixture must resolve")
        }
        XCTAssertEqual(placementsBefore.map(\.transform), placementsAfter.map(\.transform), "no photograph moved")
        XCTAssertEqual(placementsBefore.map(\.anchor), placementsAfter.map(\.anchor))
        XCTAssertEqual(placementsAfter[13].photoAssetID, "new")
        XCTAssertEqual(placementsAfter[13].caption, "caption 13")
    }
}
