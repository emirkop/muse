import XCTest
@testable import MuseApp

// MARK: - The client's mirror of the entitlement arithmetic

final class EntitlementRulesTests: XCTestCase {

    func test_isAtCapacityIsInclusiveAtTheBoundary() {
        let cases: [(used: Int, capacity: Int, atCapacity: Bool)] = [
            (0, 25, false),
            (24, 25, false),
            (25, 25, true),
            (26, 25, true),
            (0, 0, true),
        ]
        for testCase in cases {
            let entitlement = AccountEntitlement(
                state: .free, itemCapacity: testCase.capacity, itemCount: testCase.used)
            XCTAssertEqual(entitlement.isAtCapacity, testCase.atCapacity,
                           "\(testCase.used)/\(testCase.capacity)")
        }
    }

    func test_onlyAPaidAccountCannotUpgrade() {
        for state in [EntitlementState.free, .revoked, .unavailable, .unknown] {
            let entitlement = AccountEntitlement(state: state, itemCapacity: 25, itemCount: 0)
            XCTAssertTrue(entitlement.canUpgrade,
                          "\(state) must still be offered the upgrade — including unavailable, or a willing buyer is stranded")
        }
        let paid = AccountEntitlement(state: .paid, itemCapacity: 500, itemCount: 0)
        XCTAssertFalse(paid.canUpgrade, "a paid account has nothing left to buy (: one paid tier)")
    }
}

// MARK: - The client's mirror of the asset-bundle contract

final class AssetBundleContractTests: XCTestCase {

    private func file(_ assetID: String, _ role: AssetRole, bytes: Int64 = 1024) -> AssetBundleFile {
        AssetBundleFile(
            assetID: assetID, role: role,
            url: URL(string: "https://assets.example/bundles/b/v1/\(assetID)")!,
            contentType: "application/octet-stream",
            byteSize: bytes,
            checksumSHA256: String(repeating: "a", count: 64)
        )
    }

    private func manifest(_ files: [AssetBundleFile]) -> AssetBundleManifest {
        AssetBundleManifest(
            identity: AssetBundleIdentity(bundleID: "bundle_style_modern", version: 3),
            kind: .museumStyle, format: "usdz", minAppVersion: 1, files: files
        )
    }

    func test_identityAccessorsMirrorTheIdentity() {
        let subject = manifest([file("geometry", .geometry)])
        XCTAssertEqual(subject.bundleID, "bundle_style_modern")
        XCTAssertEqual(subject.version, 3)
        XCTAssertEqual(subject.identity, AssetBundleIdentity(bundleID: "bundle_style_modern", version: 3))
        XCTAssertNotEqual(
            AssetBundleIdentity(bundleID: "b", version: 1),
            AssetBundleIdentity(bundleID: "b", version: 2))
    }

    func test_roleLookupFindsTheFileOrReportsAbsence() {
        let full = manifest([file("texture", .texture), file("geometry", .geometry), file("layout", .layout)])
        XCTAssertEqual(full.geometryFile?.assetID, "geometry")
        XCTAssertEqual(full.layoutFile?.assetID, "layout")
        let geometryOnly = manifest([file("geometry", .geometry)])
        XCTAssertNil(geometryOnly.layoutFile, "a bundle with no layout must report absence")
        XCTAssertNotNil(geometryOnly.geometryFile, "sanity: the geometry file is present")
    }

    func test_totalByteSizeIsTheSumOfEveryFile() {
        let subject = manifest([
            file("geometry", .geometry, bytes: 1000),
            file("layout", .layout, bytes: 200),
            file("texture", .texture, bytes: 30),
        ])
        XCTAssertEqual(subject.totalByteSize, 1230)
        XCTAssertEqual(manifest([]).totalByteSize, 0, "an empty manifest totals nothing rather than crashing")
    }

    func test_bundleKindRawValuesMatchTheServersWireStrings() {
        let expected: [String: AssetBundleKind] = [
            "museum_style": .museumStyle,
            "room_variant": .roomVariant,
            "sculpture": .sculpture,
            "avatar": .avatar,
            "collection_design": .collectionDesign,
        ]
        for (raw, kind) in expected {
            XCTAssertEqual(AssetBundleKind(rawValue: raw), kind, "kind \(raw)")
        }
        XCTAssertNil(AssetBundleKind(rawValue: "wallpaper"))
        XCTAssertNil(AssetBundleKind(rawValue: "MUSEUM_STYLE"), "the wire form is lowercase")
    }

    func test_assetRoleRawValuesMatchTheServersWireStrings() {
        for raw in ["geometry", "layout", "material", "texture"] {
            XCTAssertNotNil(AssetRole(rawValue: raw), "role \(raw)")
        }
        XCTAssertNil(AssetRole(rawValue: "lighting"))
        XCTAssertNil(AssetRole(rawValue: "collision"))
    }

    func test_theAppAssetVersionIsAPositiveNumber() {
        XCTAssertGreaterThan(AssetBundleFormat.appAssetVersion, 0)
    }
}
