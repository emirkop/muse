import simd
import XCTest
@testable import MuseApp

final class PlaceholderRoomSlotTableTests: XCTestCase {

    func test_isNotShapedLikeACatalogVariantID() {
        XCTAssertTrue(PlaceholderRoomSlotTable.variantID.hasPrefix("fixture:"))
        XCTAssertFalse(PlaceholderRoomSlotTable.variantID.hasPrefix("style_"))
    }

    func test_coversEveryAnchorOfAFullRoom() {
        let table = PlaceholderRoomSlotTable.build()
        XCTAssertTrue(table.supportsFullRoom)
        XCTAssertEqual(table.photoTransforms.count, 28)
    }

    func test_everyAnchorLiesOnItsWall_insideTheBox() {
        let table = PlaceholderRoomSlotTable.build()
        let halfW = PlaceholderRoom.width / 2
        let halfD = PlaceholderRoom.depth / 2

        for (anchor, transform) in table.photoTransforms {
            let p = transform.position
            XCTAssertEqual(p.y, PlaceholderRoomSlotTable.mountHeight, accuracy: 0.001)
            switch anchor.wall {
            case .focal: XCTAssertEqual(p.z, -halfD + PlaceholderRoomSlotTable.wallOffset, accuracy: 0.001)
            case .rear: XCTAssertEqual(p.z, halfD - PlaceholderRoomSlotTable.wallOffset, accuracy: 0.001)
            case .left: XCTAssertEqual(p.x, -halfW + PlaceholderRoomSlotTable.wallOffset, accuracy: 0.001)
            case .right: XCTAssertEqual(p.x, halfW - PlaceholderRoomSlotTable.wallOffset, accuracy: 0.001)
            }
            XCTAssertLessThan(abs(p.x), halfW, "inside the box")
            XCTAssertLessThan(abs(p.z), halfD, "inside the box")
        }
    }

    func test_sideWallPositionsRunFromEntranceTowardsTheFocalWall() {
        let table = PlaceholderRoomSlotTable.build()
        for wall in [RoomWall.left, .right] {
            var previousZ = Float.greatestFiniteMagnitude
            for position in 0..<13 {
                let z = table.photoTransforms[SlotAnchor(wall: wall, positionOnWall: position)]!.position.z
                XCTAssertLessThan(z, previousZ, "\(wall) position \(position) must be deeper into the room than \(position - 1)")
                previousZ = z
            }
        }
    }

    func test_everyAnchorFacesIntoTheRoom() {
        let table = PlaceholderRoomSlotTable.build()
        for (anchor, transform) in table.photoTransforms {
            let facing = simd_act(transform.rotation, SIMD3<Float>(0, 0, 1))
            switch anchor.wall {
            case .focal: XCTAssertGreaterThan(facing.z, 0.99, "focal wall faces +Z")
            case .rear: XCTAssertLessThan(facing.z, -0.99, "rear wall faces −Z")
            case .left: XCTAssertGreaterThan(facing.x, 0.99, "left wall faces +X")
            case .right: XCTAssertLessThan(facing.x, -0.99, "right wall faces −X")
            }
        }
    }

    func test_resolvesEveryCountThroughThePlacementEngine() {
        let table = PlaceholderRoomSlotTable.build()
        for count in 1...Room.maxPhotos {
            let room = Room(id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
                            photoSlots: (0..<count).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") })
            guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: table) else {
                return XCTFail("count \(count) must resolve")
            }
            XCTAssertEqual(placements.count, count)
            XCTAssertEqual(placements.map(\.anchor), RoomPhotoSlotLayout.slots(forPhotoCount: count).map(\.anchor))
        }
    }
}
