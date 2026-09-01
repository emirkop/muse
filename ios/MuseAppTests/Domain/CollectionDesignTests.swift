import XCTest
@testable import MuseApp

final class CollectionDesignTests: XCTestCase {

    private func universal(_ id: String) -> CollectionDesign {
        CollectionDesign(id: id, displayName: id, categoryID: nil,
                         assetBundle: AssetBundleRef(id: "bundle_\(id)", version: 1))
    }

    private func scoped(_ id: String, _ categoryID: String) -> CollectionDesign {
        CollectionDesign(id: id, displayName: id, categoryID: categoryID,
                         assetBundle: AssetBundleRef(id: "bundle_\(id)", version: 1))
    }

    // MARK: - rule, mirrored

    func test_universalDesignAppliesToEveryCategory() {
        let design = universal("design_neutral")

        XCTAssertTrue(design.isUniversal)
        for categoryID in ["category_watches", "category_coins", "category_invented_later", nil] {
            XCTAssertTrue(
                design.applies(toCategoryID: categoryID),
                "a universal Design must apply to \(categoryID ?? "no category")"
            )
        }
    }

    func test_scopedDesignAppliesOnlyToItsOwnCategory() {
        let design = scoped("design_watch_case", "category_watches")

        XCTAssertFalse(design.isUniversal)
        XCTAssertTrue(design.applies(toCategoryID: "category_watches"))
        for categoryID in ["category_coins", "category_hot_wheels", "CATEGORY_WATCHES", nil] {
            XCTAssertFalse(
                design.applies(toCategoryID: categoryID),
                "a scoped Design must not apply to \(categoryID ?? "no category")"
            )
        }
    }

    // MARK: - Verify 7: distinct from Museum Style

    func test_collectionDesignAndMuseumStyleAreNotInterchangeable() {
        let design = universal("design_neutral")
        let style = MuseumStyle(
            id: "style_modern", displayName: "Modern",
            assetBundle: AssetBundleRef(id: "bundle_style_modern", version: 1)
        )

        XCTAssertNotEqual(
            String(describing: type(of: design)), String(describing: type(of: style)),
            "`01` §5.1 makes Collection design choices distinct from Museum styles"
        )
        XCTAssertNotEqual(design.id, style.id)

        XCTAssertTrue(design.isUniversal)

        XCTAssertEqual(design.previewSubject.assetBundle, design.assetBundle)
        XCTAssertEqual(style.previewSubject.assetBundle, style.assetBundle)
        XCTAssertNotEqual(design.previewSubject.id, style.previewSubject.id)
    }

    // MARK: - Verify 6: a bundle re-point needs no content change

    func test_aBundleRepointLeavesTheCollectionRoomUnchanged() {
        let room = CollectionRoom(
            id: "c1", name: "Watches", categoryID: "category_watches", designID: "design_neutral"
        )

        let placeholder = CollectionDesign(
            id: "design_neutral", displayName: "Development Fixture",
            categoryID: nil, isDevelopmentFixture: true,
            assetBundle: AssetBundleRef(id: "dev_fixture_collection_design", version: 1)
        )
        let authored = CollectionDesign(
            id: "design_neutral", displayName: "Vitrine",
            categoryID: nil, isDevelopmentFixture: false,
            assetBundle: AssetBundleRef(id: "bundle_authored_vitrine", version: 3)
        )

        XCTAssertEqual(placeholder.id, authored.id)
        XCTAssertNotEqual(placeholder.assetBundle, authored.assetBundle)

        XCTAssertEqual(room.designID, authored.id)
        XCTAssertEqual(authored.previewSubject.assetBundle.id, "bundle_authored_vitrine")
        XCTAssertEqual(authored.previewSubject.assetBundle.version, 3)
        XCTAssertNil(room.items.first)
    }

    func test_universalIsExpressedAsAnAbsentScope() {
        XCTAssertNil(universal("design_neutral").categoryID)
        XCTAssertEqual(scoped("design_watch_case", "category_watches").categoryID, "category_watches")
    }
}
