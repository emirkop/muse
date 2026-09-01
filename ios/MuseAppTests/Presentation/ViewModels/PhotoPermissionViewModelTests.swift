import XCTest
@testable import MuseApp

@MainActor
final class PhotoPermissionViewModelTests: XCTestCase {

    private func makeViewModel(_ provider: FakePhotoPermissionProvider) -> PhotoPermissionViewModel {
        PhotoPermissionViewModel(permissionProvider: provider)
    }

    // MARK: - Explain then prompt

    func test_undetermined_showsTheExplanationBeforeAnyPrompt() async {
        let provider = FakePhotoPermissionProvider(current: .notDetermined)
        let viewModel = makeViewModel(provider)

        await viewModel.start()

        XCTAssertEqual(viewModel.state, .explaining)
        XCTAssertEqual(provider.requestCallCount, 0, "start() must never prompt on its own")
    }

    func test_startNeverRequestsAccess_inAnyState() async {
        for access in [PhotoLibraryAccess.notDetermined, .fullAccess, .limitedAccess, .denied, .restricted] {
            let provider = FakePhotoPermissionProvider(current: access)

            await makeViewModel(provider).start()

            XCTAssertEqual(provider.requestCallCount, 0, "\(access) must not trigger a request")
        }
    }

    func test_allowAccess_promptsAndAdoptsTheOutcome() async {
        let provider = FakePhotoPermissionProvider(current: .notDetermined, afterRequest: .fullAccess)
        let viewModel = makeViewModel(provider)
        await viewModel.start()

        await viewModel.requestAccess()

        XCTAssertEqual(provider.requestCallCount, 1)
        XCTAssertEqual(viewModel.state, .granted(.fullAccess))
        XCTAssertTrue(viewModel.canProceedToSelection)
    }

    func test_requestingOutsideTheExplanationState_isRefused() async {
        let provider = FakePhotoPermissionProvider(current: .denied)
        let viewModel = makeViewModel(provider)
        await viewModel.start()

        await viewModel.requestAccess()

        XCTAssertEqual(provider.requestCallCount, 0)
        XCTAssertEqual(viewModel.state, .denied)
    }

    func test_alreadyGranted_skipsTheExplanationEntirely() async {
        for access in [PhotoLibraryAccess.fullAccess, .limitedAccess] {
            let viewModel = makeViewModel(FakePhotoPermissionProvider(current: access))

            await viewModel.start()

            XCTAssertEqual(viewModel.state, .granted(access))
            XCTAssertTrue(viewModel.canProceedToSelection)
        }
    }

    // MARK: - The three outcomes

    func test_limitedAccess_offersManageSelectedPhotos_andFullAccessDoesNot() async {
        let limited = makeViewModel(FakePhotoPermissionProvider(current: .limitedAccess))
        let full = makeViewModel(FakePhotoPermissionProvider(current: .fullAccess))

        await limited.start()
        await full.start()

        XCTAssertTrue(limited.showsManageSelectedPhotos)
        XCTAssertFalse(full.showsManageSelectedPhotos)
        XCTAssertTrue(limited.canProceedToSelection)
    }

    func test_denied_offersSettings_andBlocksSelection() async {
        let viewModel = makeViewModel(FakePhotoPermissionProvider(current: .denied))

        await viewModel.start()

        XCTAssertEqual(viewModel.state, .denied)
        XCTAssertTrue(viewModel.showsSettingsLink)
        XCTAssertFalse(viewModel.canProceedToSelection)
        XCTAssertFalse(viewModel.showsManageSelectedPhotos)
    }

    func test_restricted_withholdsTheSettingsLink() async {
        let viewModel = makeViewModel(FakePhotoPermissionProvider(current: .restricted))

        await viewModel.start()

        XCTAssertEqual(viewModel.state, .restricted)
        XCTAssertFalse(viewModel.showsSettingsLink, "Settings cannot resolve a restricted state")
        XCTAssertFalse(viewModel.canProceedToSelection)
    }

    func test_everyStateOffersAWayOutWithoutPhotos() async {
        for access in [PhotoLibraryAccess.notDetermined, .fullAccess, .limitedAccess, .denied, .restricted] {
            let viewModel = makeViewModel(FakePhotoPermissionProvider(current: access))

            await viewModel.start()

            XCTAssertTrue(viewModel.allowsContinuingWithoutPhotos, "\(access) must not be a dead end")
        }
    }

    // MARK: - Recovering from Settings

    func test_grantingInSettings_recoversTheDeniedState() async {
        let provider = FakePhotoPermissionProvider(current: .denied)
        let viewModel = makeViewModel(provider)
        await viewModel.start()
        XCTAssertEqual(viewModel.state, .denied)

        provider.current = .limitedAccess
        await viewModel.refresh()

        XCTAssertEqual(viewModel.state, .granted(.limitedAccess))
        XCTAssertEqual(provider.requestCallCount, 0, "recovery must not re-prompt")
    }

    func test_revokingInSettings_isReflectedOnReturn() async {
        let provider = FakePhotoPermissionProvider(current: .fullAccess)
        let viewModel = makeViewModel(provider)
        await viewModel.start()

        provider.current = .denied
        await viewModel.refresh()

        XCTAssertEqual(viewModel.state, .denied)
    }

    func test_foregroundingAResolvedDenial_doesNotReopenTheExplanation() async {
        let provider = FakePhotoPermissionProvider(current: .denied)
        let viewModel = makeViewModel(provider)
        await viewModel.start()

        provider.current = .notDetermined
        await viewModel.refresh()

        XCTAssertEqual(viewModel.state, .denied)
    }

    // MARK: - Layering

    func test_photolessRoomNotice_reusesExistingGateCopy() {
        XCTAssertEqual(
            PhotoPermissionViewModel.photolessRoomNotice,
            RoomCreationViewModel.zeroPhotoRoomsGateNotice,
            " must be stated once, not reworded per screen"
        )
    }

    func test_accessLevelSemantics() {
        XCTAssertTrue(PhotoLibraryAccess.notDetermined.canPresentSystemPrompt)
        for access in [PhotoLibraryAccess.fullAccess, .limitedAccess, .denied, .restricted] {
            XCTAssertFalse(access.canPresentSystemPrompt, "\(access) can no longer show a prompt")
        }
        XCTAssertTrue(PhotoLibraryAccess.denied.isResolvableInSettings)
        XCTAssertFalse(PhotoLibraryAccess.restricted.isResolvableInSettings)
    }
}

final class FakePhotoPermissionProvider: PhotoLibraryPermissionProviding, @unchecked Sendable {
    var current: PhotoLibraryAccess
    private let afterRequest: PhotoLibraryAccess?
    private(set) var requestCallCount = 0

    init(current: PhotoLibraryAccess, afterRequest: PhotoLibraryAccess? = nil) {
        self.current = current
        self.afterRequest = afterRequest
    }

    func currentAccess() async -> PhotoLibraryAccess { current }

    func requestAccess() async -> PhotoLibraryAccess {
        requestCallCount += 1
        if let afterRequest { current = afterRequest }
        return current
    }
}
