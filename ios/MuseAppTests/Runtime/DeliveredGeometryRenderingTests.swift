import RealityKit
import UIKit
import XCTest
import simd
@testable import MuseApp

@MainActor
final class DeliveredGeometryRenderingTests: XCTestCase {

    private func fixtureFile(_ name: String) -> URL? {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        let url = root
            .appendingPathComponent("assets/dev_fixtures/bundles/dev_fixture_room_variant/v1")
            .appendingPathComponent(name)
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
    }

    private func deliveredContent(photoCount: Int) throws -> RoomRuntimeContent {
        guard let geometryURL = fixtureFile("geometry.usda"),
              let layoutURL = fixtureFile("layout.json") else {
            throw XCTSkip("assets/dev_fixtures is not reachable from this test host")
        }
        let layout = try RoomVariantLayoutFile.decode(contentsOf: layoutURL)
        let geometry = RoomVariantGeometry(
            variantID: layout.table.variantID,
            identity: AssetBundleIdentity(bundleID: "dev_fixture_room_variant", version: 1),
            format: "usda",
            fileURL: geometryURL,
            entry: layout.entry
        )
        guard let content = RoomRenderingVerificationFixture.makeContent(
            photoCount: photoCount,
            deliveredGeometry: geometry,
            deliveredSlotTable: layout.table
        ) else {
            throw XCTSkip("fixture content could not be built")
        }
        return content
    }

    func test_deliveredGeometryIsMountedAndTheViewerSpawnsWhereTheBundleSays() throws {
        let content = try deliveredContent(photoCount: 4)
        guard case .variantBundle(let variant) = content.geometry else {
            return XCTFail("expected delivered geometry")
        }

        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertTrue(controller.isSceneLoaded)
        XCTAssertEqual(controller.movementController.subject, variant.entry)
        XCTAssertNotEqual(controller.movementController.subject, PlaceholderRoom.spawnPoint,
                          "a delivered Room must not inherit the placeholder box's spawn")

        XCTAssertEqual(controller.photoLayer?.mountedSlotIndices.count, 4)

        controller.viewDidDisappear(false)
        XCTAssertFalse(controller.isSceneLoaded)
    }

    func test_aDeliveredRoomDoesNotContainThePlaceholderBox() throws {
        let content = try deliveredContent(photoCount: 1)
        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        let environmentExtents = controller.environmentVisualExtentsForTesting
        XCTAssertEqual(environmentExtents?.x ?? 0, 7.15, accuracy: 0.1)
        XCTAssertEqual(environmentExtents?.z ?? 0, 9.15, accuracy: 0.1)
        XCTAssertNotEqual(environmentExtents?.x ?? 0, PlaceholderRoom.width, accuracy: 0.01)

        controller.viewDidDisappear(false)
    }

    func test_anUnloadableBundleRendersNothingRatherThanTheFixture() throws {
        guard let layoutURL = fixtureFile("layout.json") else {
            throw XCTSkip("assets/dev_fixtures is not reachable from this test host")
        }
        let layout = try RoomVariantLayoutFile.decode(contentsOf: layoutURL)
        let missing = RoomVariantGeometry(
            variantID: layout.table.variantID,
            identity: AssetBundleIdentity(bundleID: "dev_fixture_room_variant", version: 1),
            format: "usda",
            fileURL: URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("does-not-exist.usda"),
            entry: layout.entry
        )
        let content = try XCTUnwrap(RoomRenderingVerificationFixture.makeContent(
            photoCount: 1, deliveredGeometry: missing, deliveredSlotTable: layout.table
        ))

        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)

        XCTAssertTrue(controller.isSceneLoaded, "the scene still stands up; it is simply empty")
        let extents = controller.environmentVisualExtentsForTesting ?? SIMD3<Float>(repeating: 0)
        XCTAssertEqual(extents.x, 0, accuracy: 0.001, "no environment geometry")
        XCTAssertNotEqual(extents.x, PlaceholderRoom.width, accuracy: 0.01,
                          "and emphatically not the placeholder box")

        controller.viewDidDisappear(false)
    }

    func test_theFixturePathStillUsesThePlaceholderBox() {
        let content = RoomRenderingVerificationFixture.makeContent(photoCount: 3)!
        XCTAssertEqual(content.geometry, .verificationFixture)

        let controller = RealityKitSceneViewController(content: content)
        controller.loadViewIfNeeded()
        controller.viewWillAppear(false)
        XCTAssertEqual(controller.movementController.subject, PlaceholderRoom.spawnPoint)
        controller.viewDidDisappear(false)
    }
}
