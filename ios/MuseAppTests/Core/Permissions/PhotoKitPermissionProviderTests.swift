import XCTest
import Photos
@testable import MuseApp

final class PhotoKitPermissionProviderTests: XCTestCase {

    func test_mapsEveryPhotoKitStatusToItsProductMeaning() {
        let expected: [(PHAuthorizationStatus, PhotoLibraryAccess)] = [
            (.notDetermined, .notDetermined),
            (.restricted, .restricted),
            (.denied, .denied),
            (.authorized, .fullAccess),
            (.limited, .limitedAccess)
        ]

        for (status, access) in expected {
            XCTAssertEqual(
                PhotoKitPermissionProvider.map(status), access,
                "PHAuthorizationStatus.\(status) must map to \(access)"
            )
        }
    }

    func test_limitedIsNeverCollapsedIntoFullAccess() {
        XCTAssertNotEqual(PhotoKitPermissionProvider.map(.limited), .fullAccess)
        XCTAssertTrue(PhotoKitPermissionProvider.map(.limited).allowsPhotoSelection)
    }

    func test_restrictedIsNeverCollapsedIntoDenied() {
        XCTAssertNotEqual(PhotoKitPermissionProvider.map(.restricted), .denied)
        XCTAssertFalse(PhotoKitPermissionProvider.map(.restricted).isResolvableInSettings)
    }

    func test_currentAccessHasNoSideEffects() async {
        let provider = PhotoKitPermissionProvider()

        let first = await provider.currentAccess()
        let second = await provider.currentAccess()

        XCTAssertEqual(first, second)
    }
}
