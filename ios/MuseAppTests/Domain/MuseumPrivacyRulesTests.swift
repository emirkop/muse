import XCTest
@testable import MuseApp

final class MuseumPrivacyRulesTests: XCTestCase {

    // MARK: - two-flag table

    func test_visitorVisibility_coversEveryCell() {
        XCTAssertEqual(MuseumPrivacyRules.visitorVisibility(museum: .public, room: .public), .visible)
        XCTAssertEqual(MuseumPrivacyRules.visitorVisibility(museum: .public, room: .private), .hiddenByRoom)
        XCTAssertEqual(MuseumPrivacyRules.visitorVisibility(museum: .private, room: .private), .hiddenByMuseum)
        XCTAssertEqual(MuseumPrivacyRules.visitorVisibility(museum: .private, room: .public), .hiddenByMuseum)
    }

    func test_museumReachability_isTheMuseumLevelGate() {
        XCTAssertTrue(MuseumPrivacyRules.museumIsReachableByVisitors(.public))
        XCTAssertFalse(MuseumPrivacyRules.museumIsReachableByVisitors(.private))
    }

    // MARK: - Exposure confirmation (`02` step 2)

    func test_museumConfirmation_isRequiredOnlyWhenGoingPublic() {
        XCTAssertTrue(MuseumPrivacyRules.museumChangeNeedsExposureConfirmation(from: .private, to: .public))
        XCTAssertFalse(MuseumPrivacyRules.museumChangeNeedsExposureConfirmation(from: .public, to: .private),
                       "removing access is not oversharing")
        XCTAssertFalse(MuseumPrivacyRules.museumChangeNeedsExposureConfirmation(from: .public, to: .public))
        XCTAssertFalse(MuseumPrivacyRules.museumChangeNeedsExposureConfirmation(from: .private, to: .private))
    }

    func test_roomConfirmation_dependsOnTheMuseumBeingReachable() {
        XCTAssertTrue(MuseumPrivacyRules.roomChangeNeedsExposureConfirmation(
            museum: .public, from: .private, to: .public))
        XCTAssertFalse(MuseumPrivacyRules.roomChangeNeedsExposureConfirmation(
            museum: .private, from: .private, to: .public))
        XCTAssertFalse(MuseumPrivacyRules.roomChangeNeedsExposureConfirmation(
            museum: .public, from: .public, to: .private))
    }

    func test_roomsExposedByMakingMuseumPublic_countsOnlyPublicRooms() {
        let rooms = [
            Room(id: "a", name: "A", variantID: "v", privacy: .public),
            Room(id: "b", name: "B", variantID: "v", privacy: .private),
            Room(id: "c", name: "C", variantID: "v", privacy: .public)
        ]

        XCTAssertEqual(MuseumPrivacyRules.roomsExposedByMakingMuseumPublic(rooms), 2)
        XCTAssertEqual(MuseumPrivacyRules.roomsExposedByMakingMuseumPublic([]), 0)
    }

    // MARK: - RoomPatch

    func test_privacyPatch_carriesNothingElse() {
        let patch = RoomPatch.privacy(.public)

        XCTAssertEqual(patch.privacy, .public)
        XCTAssertNil(patch.name, "a privacy change must not be able to rewrite the name")
        XCTAssertNil(patch.variantID, "a privacy change must not be able to rewrite the design")
        XCTAssertFalse(patch.isEmpty)
    }

    func test_variantPatch_carriesNothingElse() {
        let patch = RoomPatch.variant("v2")

        XCTAssertEqual(patch.variantID, "v2")
        XCTAssertNil(patch.privacy, "a design change must not be able to alter privacy")
        XCTAssertNil(patch.name)
    }

    func test_emptyPatch_isRecognisable() {
        XCTAssertTrue(RoomPatch().isEmpty)
        XCTAssertFalse(RoomPatch(name: "x").isEmpty)
    }
}
