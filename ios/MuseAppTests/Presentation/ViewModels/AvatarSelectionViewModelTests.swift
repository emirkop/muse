import XCTest
@testable import MuseApp

@MainActor
final class AvatarSelectionViewModelTests: XCTestCase {
    func test_selectAvatar_success_savesAndDoesNotTouchDisplayName() async {
        let service = FakeProfileService()
        service.result = .success(Profile(displayName: "Ada", avatarID: "avatar_3"))
        let viewModel = AvatarSelectionViewModel(profileService: service, accessToken: "token-1")

        await viewModel.selectAvatar("avatar_3")

        XCTAssertEqual(viewModel.state, .saved(avatarID: "avatar_3"))
        XCTAssertEqual(service.receivedAvatarIDs, ["avatar_3"])
        XCTAssertEqual(service.receivedDisplayNames, [nil], "an avatar-only save must not touch display_name")
        XCTAssertEqual(service.receivedAccessTokens, ["token-1"])
    }

    func test_selectAvatar_failure_setsFailedState() async {
        let service = FakeProfileService()
        service.result = .failure(IdentityAPIClientError.server(statusCode: 400, message: "invalid avatar_id"))
        let viewModel = AvatarSelectionViewModel(profileService: service, accessToken: "token-2")

        await viewModel.selectAvatar("avatar_1")

        guard case .failed = viewModel.state else {
            XCTFail("expected .failed state, got \(viewModel.state)")
            return
        }
    }
}
