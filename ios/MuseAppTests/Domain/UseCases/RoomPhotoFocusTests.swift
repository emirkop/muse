import simd
import XCTest
@testable import MuseApp

final class RoomPhotoFocusTests: XCTestCase {

    private func focalWallPhoto(_ id: String, slot: Int = 0, x: Float = 0, z: Float = -4) -> ResolvedPhotoPlacement {
        ResolvedPhotoPlacement(
            slotIndex: slot,
            photoAssetID: id,
            caption: "",
            anchor: SlotAnchor(wall: .focal, positionOnWall: 0),
            transform: SlotTransform(
                position: SIMD3<Float>(x, 1.55, z),
                rotation: simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0)),
                scale: SIMD3<Float>(1.6, 1.2, 1)
            )
        )
    }

    private func leftWallPhoto(_ id: String, slot: Int, z: Float) -> ResolvedPhotoPlacement {
        ResolvedPhotoPlacement(
            slotIndex: slot,
            photoAssetID: id,
            caption: "",
            anchor: SlotAnchor(wall: .left, positionOnWall: slot),
            transform: SlotTransform(
                position: SIMD3<Float>(-4, 1.55, z),
                rotation: simd_quatf(angle: .pi / 2, axis: SIMD3<Float>(0, 1, 0)),
                scale: SIMD3<Float>(0.62, 0.62, 1)
            )
        )
    }

    private let eye = SIMD3<Float>(0, 1.6, 0)
    private let lookingForward = SIMD3<Float>(0, 0, -1)

    // MARK: - Proximity

    func test_noPhotographs_focusesNothing() {
        XCTAssertNil(RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: []))
    }

    func test_standingCloseAndLookingAtIt_focusesIt() {
        let photo = focalWallPhoto("a", z: -2)

        let focused = RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: [photo])

        XCTAssertEqual(focused?.photoAssetID, "a")
        XCTAssertEqual(focused?.slotIndex, 0)
    }

    func test_tooFarAway_focusesNothing_evenLookingStraightAtIt() {
        let photo = focalWallPhoto("a", z: -(RoomPhotoFocus.interimFocusRadiusMetres + 1.5))

        XCTAssertNil(
            RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: [photo]),
            "`02` says *up close* — focus must not reach across the Room"
        )
    }

    func test_distanceIsMeasuredInThreeDimensions() {
        let overhead = ResolvedPhotoPlacement(
            slotIndex: 0, photoAssetID: "a", caption: "",
            anchor: SlotAnchor(wall: .focal, positionOnWall: 0),
            transform: SlotTransform(
                position: SIMD3<Float>(0, 12, -0.5),
                rotation: simd_quatf(angle: 0, axis: SIMD3<Float>(0, 1, 0)),
                scale: .one
            )
        )

        XCTAssertNil(
            RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: [overhead]),
            "height counts towards distance, unlike LobbyCardFocus's floor-level cards"
        )
    }

    // MARK: - Camera direction

    func test_closeButLookingAway_focusesNothing() {
        let photo = focalWallPhoto("a", z: -1.5)

        let focused = RoomPhotoFocus.focusedPhoto(
            eyePosition: eye,
            forward: SIMD3<Float>(0, 0, 1),
            placements: [photo]
        )

        XCTAssertNil(focused, "proximity alone is not the rule — `02` says proximity *and* camera direction")
    }

    func test_lookingSidewaysPastAPhotograph_focusesNothing() {
        let photo = focalWallPhoto("a", z: -1.5)

        XCTAssertNil(
            RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: SIMD3<Float>(1, 0, 0), placements: [photo])
        )
    }

    func test_alignmentIsReported() throws {
        let photo = focalWallPhoto("a", z: -2)

        let focused = try XCTUnwrap(
            RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: [photo])
        )

        XCTAssertGreaterThan(focused.alignment, 0.98, "dead ahead, barely off-axis from the small height difference")
        XCTAssertEqual(focused.distance, length(photo.transform.position - eye), accuracy: 0.001)
    }

    func test_aZeroLengthViewDirection_focusesNothingRatherThanCrashing() {
        XCTAssertNil(
            RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: .zero, placements: [focalWallPhoto("a", z: -2)])
        )
    }

    // MARK: - Front side

    func test_standingBehindAPhotograph_focusesNothing() {
        let photo = focalWallPhoto("a", z: -2)
        let behind = SIMD3<Float>(0, 1.6, -3)

        XCTAssertNil(
            RoomPhotoFocus.focusedPhoto(eyePosition: behind, forward: SIMD3<Float>(0, 0, 1), placements: [photo])
        )
    }

    func test_leftWallPhotographFocusesFromInsideTheRoom() {
        let photo = leftWallPhoto("a", slot: 1, z: 0)
        let standingNear = SIMD3<Float>(-2.2, 1.6, 0)

        let focused = RoomPhotoFocus.focusedPhoto(
            eyePosition: standingNear,
            forward: SIMD3<Float>(-1, 0, 0),
            placements: [photo]
        )

        XCTAssertEqual(focused?.photoAssetID, "a")
    }

    // MARK: - Choosing between candidates

    func test_prefersTheMostCentredNotTheNearest() throws {
        let near = focalWallPhoto("near", slot: 1, x: 1.2, z: -1.0)
        let centred = focalWallPhoto("centred", slot: 2, x: 0, z: -1.8)

        let focused = try XCTUnwrap(
            RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: [near, centred])
        )

        XCTAssertEqual(focused.photoAssetID, "centred")
    }

    func test_equallyAlignedAndEquallyNear_resolvesDeterministically() {
        let left = focalWallPhoto("left", slot: 5, x: -0.5, z: -2)
        let right = focalWallPhoto("right", slot: 3, x: 0.5, z: -2)

        let first = RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: [left, right])
        let reversed = RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: [right, left])

        XCTAssertEqual(first?.photoAssetID, reversed?.photoAssetID, "order of input must not change the verdict")
        XCTAssertEqual(first?.slotIndex, 3, "the lower slot index breaks a perfect tie")
    }

    // MARK: - Hysteresis

    func test_focusDoesNotFlickerBetweenNeighbours() {
        let a = leftWallPhoto("a", slot: 0, z: 0.36)
        let b = leftWallPhoto("b", slot: 1, z: -0.36)
        let facingWall = SIMD3<Float>(-1, 0, 0)

        var current = RoomPhotoFocus.focusedPhoto(
            eyePosition: SIMD3<Float>(-2.5, 1.6, 0), forward: facingWall, placements: [a, b]
        )?.photoAssetID
        let firstVerdict = current

        var switches = 0
        for step in 0...40 {
            let z = 0.02 - Float(step) * 0.001
            let next = RoomPhotoFocus.focusedPhoto(
                eyePosition: SIMD3<Float>(-2.5, 1.6, z),
                forward: facingWall,
                placements: [a, b],
                currentlyFocused: current
            )?.photoAssetID
            if next != current { switches += 1 }
            current = next
        }

        XCTAssertNotNil(firstVerdict)
        XCTAssertLessThanOrEqual(switches, 1, "focus must not oscillate as the viewer drifts past the midpoint")
    }

    func test_turningToAnotherPhotograph_movesFocus() {
        let standing = SIMD3<Float>(-2, 1.6, 0)
        let ahead = focalWallPhoto("ahead", slot: 0, x: -2, z: -2)
        let side = leftWallPhoto("side", slot: 1, z: 0)

        let focusedAhead = RoomPhotoFocus.focusedPhoto(
            eyePosition: standing, forward: SIMD3<Float>(0, 0, -1),
            placements: [ahead, side], currentlyFocused: nil
        )
        let afterTurning = RoomPhotoFocus.focusedPhoto(
            eyePosition: standing, forward: SIMD3<Float>(-1, 0, 0),
            placements: [ahead, side], currentlyFocused: focusedAhead?.photoAssetID
        )

        XCTAssertEqual(focusedAhead?.photoAssetID, "ahead", "looking forward focuses the one ahead")
        XCTAssertEqual(afterTurning?.photoAssetID, "side", "hysteresis must not outweigh a deliberate turn")
    }

    func test_walkingAway_releasesFocusAtTheLooserRadius() {
        let photo = focalWallPhoto("a", z: 0)
        let justInsideRelease = RoomPhotoFocus.interimReleaseRadiusMetres - 0.1
        let outsideRelease = RoomPhotoFocus.interimReleaseRadiusMetres + 0.1

        let held = RoomPhotoFocus.focusedPhoto(
            eyePosition: SIMD3<Float>(0, 1.55, justInsideRelease),
            forward: SIMD3<Float>(0, 0, -1), placements: [photo], currentlyFocused: "a"
        )
        let released = RoomPhotoFocus.focusedPhoto(
            eyePosition: SIMD3<Float>(0, 1.55, outsideRelease),
            forward: SIMD3<Float>(0, 0, -1), placements: [photo], currentlyFocused: "a"
        )

        XCTAssertEqual(held?.photoAssetID, "a", "focus is kept out past the radius that could take it")
        XCTAssertNil(released)
    }

    func test_anIncumbentThatIsGone_doesNotResurrect() {
        let remaining = focalWallPhoto("b", slot: 1, x: 3.0, z: -0.2)

        let focused = RoomPhotoFocus.focusedPhoto(
            eyePosition: eye, forward: lookingForward, placements: [remaining], currentlyFocused: "deleted-a"
        )

        XCTAssertNil(focused?.photoAssetID == "deleted-a" ? focused : nil)
    }

    func test_passingNoIncumbentEvaluatesTheRuleCold() {
        let a = focalWallPhoto("a", slot: 0, x: 0, z: -2)
        let b = focalWallPhoto("b", slot: 1, x: 1.0, z: -2)

        let cold = RoomPhotoFocus.focusedPhoto(eyePosition: eye, forward: lookingForward, placements: [a, b])
        let withNilIncumbent = RoomPhotoFocus.focusedPhoto(
            eyePosition: eye, forward: lookingForward, placements: [a, b], currentlyFocused: nil
        )

        XCTAssertEqual(cold?.photoAssetID, withNilIncumbent?.photoAssetID)
    }

    // MARK: - Against the real fixture layout

    func test_inAFull28PhotoRoom_atMostOneIsFocusedFromAnyStandingPoint() {
        let room = Room(
            id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: (0..<28).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") }
        )
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(
            room: room, slotTable: PlaceholderRoomSlotTable.build()
        ) else { return XCTFail("the fixture Room must resolve") }

        let facings: [SIMD3<Float>] = [
            SIMD3(0, 0, -1), SIMD3(0, 0, 1), SIMD3(-1, 0, 0), SIMD3(1, 0, 0)
        ]
        var everFocused = 0
        for xStep in -3...3 {
            for zStep in -4...4 {
                let eye = SIMD3<Float>(Float(xStep), 1.6, Float(zStep))
                for facing in facings {
                    let focused = RoomPhotoFocus.focusedPhoto(
                        eyePosition: eye, forward: facing, placements: placements
                    )
                    if let focused {
                        everFocused += 1
                        XCTAssertTrue(
                            placements.contains { $0.photoAssetID == focused.photoAssetID },
                            "focus must name a photograph that is actually mounted"
                        )
                    }
                }
            }
        }

        XCTAssertGreaterThan(everFocused, 0, "somewhere in a 28-photo Room, something must focus")
    }

    func test_focusReachesEveryWallOfTheFixtureRoom() {
        let room = Room(
            id: "r", name: "n", variantID: PlaceholderRoomSlotTable.variantID, privacy: .private,
            photoSlots: (0..<28).map { PhotoSlotAssignment(slotIndex: $0, photoAssetID: "a\($0)", caption: "") }
        )
        guard case .resolved(let placements) = RoomPlacementResolver.resolve(
            room: room, slotTable: PlaceholderRoomSlotTable.build()
        ) else { return XCTFail("the fixture Room must resolve") }

        var wallsFocused: Set<RoomWall> = []
        let byAsset = Dictionary(uniqueKeysWithValues: placements.map { ($0.photoAssetID, $0) })

        for placement in placements {
            let normal = placement.transform.rotation.act(SIMD3<Float>(0, 0, 1))
            let standing = placement.transform.position + normal * 2
            let focused = RoomPhotoFocus.focusedPhoto(
                eyePosition: standing, forward: -normal, placements: placements
            )
            if let focused, let hit = byAsset[focused.photoAssetID] {
                wallsFocused.insert(hit.anchor.wall)
            }
        }

        XCTAssertEqual(
            wallsFocused, Set(RoomWall.allCases),
            "a photograph on every wall must be focusable when stood in front of"
        )
    }
}
