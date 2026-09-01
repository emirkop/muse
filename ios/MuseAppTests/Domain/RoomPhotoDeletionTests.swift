import XCTest
@testable import MuseApp

final class RoomPhotoDeletionTests: XCTestCase {

    private func slots(_ count: Int) -> [PhotoSlotAssignment] {
        (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "asset_\($0)", caption: "caption \($0)") }
    }

    func test_removing_compactsTheRemainingSlots_keepingOrderAndCaptions() {
        let result = RoomPhotoDeletion.removing("asset_2", from: slots(5))

        XCTAssertEqual(result.map(\.photoAssetID), ["asset_0", "asset_1", "asset_3", "asset_4"])
        XCTAssertEqual(result.map(\.slotIndex), [0, 1, 2, 3], "no gap: indices are contiguous again")
        XCTAssertEqual(result.map(\.caption), ["caption 0", "caption 1", "caption 3", "caption 4"], "every caption stays with its photograph")
    }

    func test_removing_first_last_andOnly() {
        XCTAssertEqual(RoomPhotoDeletion.removing("asset_0", from: slots(3)).map(\.photoAssetID), ["asset_1", "asset_2"])
        XCTAssertEqual(RoomPhotoDeletion.removing("asset_2", from: slots(3)).map(\.photoAssetID), ["asset_0", "asset_1"])
        XCTAssertEqual(RoomPhotoDeletion.removing("asset_0", from: slots(1)), [])
        XCTAssertEqual(RoomPhotoDeletion.removing("asset_0", from: slots(3)).map(\.slotIndex), [0, 1])
    }

    func test_removing_anUnknownPhotograph_changesNothingButNormalises() {
        let before = slots(3)
        XCTAssertEqual(RoomPhotoDeletion.removing("ghost", from: before), before)
    }

    func test_removing_worksOnAnUnorderedInput() {
        let shuffled = [
            PhotoSlotAssignment(slotIndex: 2, photoAssetID: "c", caption: "C"),
            PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a", caption: "A"),
            PhotoSlotAssignment(slotIndex: 1, photoAssetID: "b", caption: "B")
        ]
        let result = RoomPhotoDeletion.removing("b", from: shuffled)

        XCTAssertEqual(result.map(\.photoAssetID), ["a", "c"])
        XCTAssertEqual(result.map(\.slotIndex), [0, 1])
        XCTAssertEqual(result.map(\.caption), ["A", "C"])
    }

    func test_removing_resolvesThroughThePlacementEngineForTheNewCount_includingTheReflow() {
        let table = PlaceholderRoomSlotTable.build()
        let before = Room(id: "r", name: "n", variantID: table.variantID, privacy: .private, photoSlots: slots(5))
        let after = before.replacingPhotoSlots(RoomPhotoDeletion.removing("asset_1", from: before.photoSlots))

        guard case .resolved(let placementsBefore) = RoomPlacementResolver.resolve(room: before, slotTable: table),
              case .resolved(let placementsAfter) = RoomPlacementResolver.resolve(room: after, slotTable: table) else {
            return XCTFail("fixture must resolve")
        }
        XCTAssertEqual(placementsAfter.count, 4)
        XCTAssertEqual(placementsAfter.map(\.photoAssetID), ["asset_0", "asset_2", "asset_3", "asset_4"])
        XCTAssertFalse(placementsBefore.contains { $0.anchor.wall == .rear })
        XCTAssertTrue(placementsAfter.contains { $0.anchor.wall == .rear }, "the layout reflowed for the new count")
        let fresh = Room(id: "r", name: "n", variantID: table.variantID, privacy: .private, photoSlots: after.photoSlots)
        guard case .resolved(let freshPlacements) = RoomPlacementResolver.resolve(room: fresh, slotTable: table) else {
            return XCTFail("must resolve")
        }
        XCTAssertEqual(placementsAfter, freshPlacements)
    }
}
