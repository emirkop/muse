import XCTest
import simd
@testable import MuseApp

final class RoomPhotoSlotLayoutTests: XCTestCase {

    // MARK: - Properties that must hold at every supported count

    func test_everyCountFrom1To28_satisfiesTheConfirmedPlacementRule() {
        for count in 1...Room.maxPhotos {
            let slots = RoomPhotoSlotLayout.slots(forPhotoCount: count)

            XCTAssertEqual(slots.count, count, "count \(count): every photo must get a slot")

            XCTAssertEqual(slots.map(\.index), Array(0..<count), "count \(count): indices must be 0..<count in order")

            XCTAssertEqual(slots[0].wall, .focal, "count \(count): slot 0 must be focal")
            XCTAssertEqual(
                slots.filter { $0.wall == .focal }.count, 1,
                "count \(count): exactly one focal photo"
            )

            let expectsRear = (count - 1) % 2 == 1
            let rearCount = slots.filter { $0.wall == .rear }.count
            XCTAssertEqual(rearCount, expectsRear ? 1 : 0, "count \(count): rear-wall activation")

            if expectsRear {
                XCTAssertEqual(slots.last?.wall, .rear, "count \(count): rear must be the final slot")
            }

            let left = slots.filter { $0.wall == .left }.count
            let right = slots.filter { $0.wall == .right }.count
            XCTAssertEqual(left, right, "count \(count): side walls must hold equal counts")

            XCTAssertEqual(
                Set(slots.map(\.anchor)).count, count,
                "count \(count): anchors must be unique"
            )

            for wall in RoomWall.allCases {
                let positions = slots.filter { $0.wall == wall }.map(\.positionOnWall).sorted()
                XCTAssertEqual(
                    positions, Array(0..<positions.count),
                    "count \(count): \(wall.rawValue) positions must be contiguous from 0"
                )
            }
        }
    }

    func test_sideWallPhotosStrictlyAlternate_startingLeft() {
        for count in 1...Room.maxPhotos {
            let sideWalls = RoomPhotoSlotLayout.slots(forPhotoCount: count)
                .map(\.wall)
                .filter { $0 == .left || $0 == .right }

            for (offset, wall) in sideWalls.enumerated() {
                let expected: RoomWall = offset % 2 == 0 ? .left : .right
                XCTAssertEqual(wall, expected, "count \(count): side photo \(offset) wall")
            }
        }
    }

    func test_ad003_photoTwoGoesToTheLeftWall() {
        XCTAssertEqual(RoomPhotoSlotLayout.firstAlternatingWall, .left)

        let slots = RoomPhotoSlotLayout.slots(forPhotoCount: 3)
        XCTAssertEqual(slots[1].wall, .left, "photo #2 must take the left wall")
        XCTAssertEqual(slots[2].wall, .right, "photo #3 must take the right wall")
    }

    // MARK: - The exact table, spelled out

    func test_lowCounts_matchTheConfirmedTableExactly() {
        let expected: [Int: [(RoomWall, Int)]] = [
            1: [(.focal, 0)],
            2: [(.focal, 0), (.rear, 0)],
            3: [(.focal, 0), (.left, 0), (.right, 0)],
            4: [(.focal, 0), (.left, 0), (.right, 0), (.rear, 0)],
            5: [(.focal, 0), (.left, 0), (.right, 0), (.left, 1), (.right, 1)],
            6: [(.focal, 0), (.left, 0), (.right, 0), (.left, 1), (.right, 1), (.rear, 0)]
        ]

        for (count, table) in expected.sorted(by: { $0.key < $1.key }) {
            let slots = RoomPhotoSlotLayout.slots(forPhotoCount: count)
            XCTAssertEqual(slots.count, table.count, "count \(count)")
            for (index, entry) in table.enumerated() {
                XCTAssertEqual(slots[index].wall, entry.0, "count \(count), slot \(index) wall")
                XCTAssertEqual(slots[index].positionOnWall, entry.1, "count \(count), slot \(index) position")
            }
        }
    }

    func test_fullRoom_uses13PhotosPerSideWallPlusFocalAndRear() {
        let slots = RoomPhotoSlotLayout.slots(forPhotoCount: 28)

        XCTAssertEqual(slots.filter { $0.wall == .focal }.count, 1)
        XCTAssertEqual(slots.filter { $0.wall == .left }.count, 13)
        XCTAssertEqual(slots.filter { $0.wall == .right }.count, 13)
        XCTAssertEqual(slots.filter { $0.wall == .rear }.count, 1)
        XCTAssertEqual(RoomPhotoSlotLayout.requiredAnchorsForFullRoom.count, 28)
    }

    func test_27Photos_needNoRearWall() {
        let slots = RoomPhotoSlotLayout.slots(forPhotoCount: 27)

        XCTAssertTrue(slots.allSatisfy { $0.wall != .rear })
        XCTAssertEqual(slots.filter { $0.wall == .left }.count, 13)
        XCTAssertEqual(slots.filter { $0.wall == .right }.count, 13)
    }

    // MARK: - Determinism and bounds

    func test_layoutIsDeterministic_acrossRepeatedEvaluation() {
        for count in 1...Room.maxPhotos {
            let first = RoomPhotoSlotLayout.slots(forPhotoCount: count)
            let second = RoomPhotoSlotLayout.slots(forPhotoCount: count)
            XCTAssertEqual(first, second, "count \(count) must be reproducible")
        }
    }

    func test_zeroPhotos_yieldsAnEmptyLayout() {
        XCTAssertTrue(RoomPhotoSlotLayout.supports(photoCount: 0))
        XCTAssertTrue(RoomPhotoSlotLayout.slots(forPhotoCount: 0).isEmpty)
    }

    func test_countsBeyondTheConfirmedCap_areUnsupported() {
        XCTAssertFalse(RoomPhotoSlotLayout.supports(photoCount: 29))
        XCTAssertFalse(RoomPhotoSlotLayout.supports(photoCount: -1))
        XCTAssertTrue(RoomPhotoSlotLayout.slots(forPhotoCount: 29).isEmpty)
    }

    // MARK: - Sculptures

    func test_sculptureSlots_areBoundedByTheConfirmedCap() {
        XCTAssertEqual(RoomSculptureSlotLayout.slotCount, Room.maxSculptures)
        XCTAssertTrue(RoomSculptureSlotLayout.isValid(slotIndex: Room.maxSculptures - 1))
        XCTAssertFalse(RoomSculptureSlotLayout.isValid(slotIndex: Room.maxSculptures))
        XCTAssertNil(
            RoomSculptureSlotLayout.lowestFreeSlot(
                occupied: (0..<Room.maxSculptures).map { SculptureInstance(slotIndex: $0, catalogID: "s") }
            ),
            "a Room at the cap has no free slot"
        )
    }
}

final class RoomPlacementResolverTests: XCTestCase {

    private func makeTable(variantID: String = "variant_a") -> RoomVariantSlotTable {
        var transforms: [SlotAnchor: SlotTransform] = [:]
        for (offset, anchor) in RoomPhotoSlotLayout.requiredAnchorsForFullRoom.enumerated() {
            transforms[anchor] = SlotTransform(position: SIMD3<Float>(Float(offset), 0, 0))
        }
        return RoomVariantSlotTable(variantID: variantID, photoTransforms: transforms)
    }

    private func makeRoom(photoCount: Int, variantID: String = "variant_a") -> Room {
        Room(
            id: "room_1",
            name: "Trabzon",
            variantID: variantID,
            privacy: .private,
            photoSlots: (0..<photoCount).map {
                PhotoSlotAssignment(slotIndex: $0, photoAssetID: "photo_\($0)", caption: "")
            }
        )
    }

    func test_withNoSlotTable_reportsUnavailable_ratherThanInventingTransforms() {
        let resolution = RoomPlacementResolver.resolve(room: makeRoom(photoCount: 5), slotTable: nil)

        XCTAssertEqual(resolution, .unresolvable(.slotTableUnavailable(variantID: "variant_a")))
    }

    func test_productionSlotTableProvider_reportsNoTable() async {
        let table = await UnavailableSlotTableProvider().slotTable(forVariantID: "variant_a")

        XCTAssertNil(table)
    }

    func test_resolvesEveryPhoto_toItsLayoutAnchorTransform() {
        let table = makeTable()

        for count in 1...Room.maxPhotos {
            let resolution = RoomPlacementResolver.resolve(room: makeRoom(photoCount: count), slotTable: table)

            guard case .resolved(let placements) = resolution else {
                XCTFail("count \(count): expected .resolved, got \(resolution)")
                continue
            }
            XCTAssertEqual(placements.count, count)

            let layout = RoomPhotoSlotLayout.slots(forPhotoCount: count)
            for (placement, slot) in zip(placements, layout) {
                XCTAssertEqual(placement.anchor, slot.anchor, "count \(count), slot \(slot.index)")
                XCTAssertEqual(placement.transform, table.photoTransforms[slot.anchor])
                XCTAssertEqual(placement.photoAssetID, "photo_\(placement.slotIndex)")
            }
        }
    }

    func test_tableFromADifferentVariant_isRefused() {
        let resolution = RoomPlacementResolver.resolve(
            room: makeRoom(photoCount: 3, variantID: "variant_a"),
            slotTable: makeTable(variantID: "variant_b")
        )

        XCTAssertEqual(resolution, .unresolvable(.variantMismatch(expected: "variant_a", received: "variant_b")))
    }

    func test_tableMissingAnAnchor_isReportedRatherThanSkippingThePhoto() {
        let full = makeTable()
        var partial = full.photoTransforms
        partial[SlotAnchor(wall: .right, positionOnWall: 0)] = nil
        let table = RoomVariantSlotTable(variantID: "variant_a", photoTransforms: partial)

        let resolution = RoomPlacementResolver.resolve(room: makeRoom(photoCount: 3), slotTable: table)

        XCTAssertEqual(
            resolution,
            .unresolvable(.anchorMissingFromTable(SlotAnchor(wall: .right, positionOnWall: 0)))
        )
        XCTAssertFalse(table.supportsFullRoom)
        XCTAssertTrue(full.supportsFullRoom)
    }

    func test_nonContiguousSlotIndices_stillPlaceInOrder_withNoGapInTheRoom() {
        let room = Room(
            id: "room_1",
            name: "Trabzon",
            variantID: "variant_a",
            privacy: .private,
            photoSlots: [
                PhotoSlotAssignment(slotIndex: 0, photoAssetID: "a", caption: ""),
                PhotoSlotAssignment(slotIndex: 7, photoAssetID: "b", caption: ""),
                PhotoSlotAssignment(slotIndex: 9, photoAssetID: "c", caption: "")
            ]
        )

        guard case .resolved(let placements) = RoomPlacementResolver.resolve(room: room, slotTable: makeTable()) else {
            XCTFail("expected .resolved")
            return
        }
        XCTAssertEqual(placements.map(\.photoAssetID), ["a", "b", "c"])
        XCTAssertEqual(placements.map(\.anchor.wall), [.focal, .left, .right])
    }

    func test_resolutionIsIndependentOfTheOrderContentArrivesIn() {
        let table = makeTable()
        let ordered = makeRoom(photoCount: 6)
        let shuffled = Room(
            id: ordered.id,
            name: ordered.name,
            variantID: ordered.variantID,
            privacy: ordered.privacy,
            photoSlots: ordered.photoSlots.reversed()
        )

        XCTAssertEqual(
            RoomPlacementResolver.resolve(room: ordered, slotTable: table),
            RoomPlacementResolver.resolve(room: shuffled, slotTable: table)
        )
    }

    func test_moreThanTheConfirmedCap_isRefused() {
        let resolution = RoomPlacementResolver.resolve(room: makeRoom(photoCount: 29), slotTable: makeTable())

        XCTAssertEqual(resolution, .unresolvable(.unsupportedPhotoCount(29)))
    }
}
