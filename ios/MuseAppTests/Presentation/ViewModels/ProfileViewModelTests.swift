import XCTest
@testable import MuseApp

@MainActor
final class ProfileViewModelTests: XCTestCase {
    func test_load_ownProfile_isEditableAndFetchesOwnProfile() async {
        let service = FakeProfileService()
        service.result = .success(Profile(displayName: "Ada", avatarID: ""))
        let viewModel = ProfileViewModel(profileService: service, accessToken: "token-1")

        XCTAssertTrue(viewModel.isEditable)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .loaded(Profile(displayName: "Ada", avatarID: "")))
        XCTAssertEqual(service.receivedAccessTokens, ["token-1"])
        XCTAssertTrue(service.receivedAccountIDs.isEmpty, "fetching one's own profile must not call fetchProfile(accountID:)")
    }

    func test_load_otherAccountProfile_isNotEditableAndFetchesByAccountID() async {
        let service = FakeProfileService()
        service.result = .success(Profile(displayName: "Museum Owner", avatarID: ""))
        let viewModel = ProfileViewModel(profileService: service, accessToken: "token-2", accountID: "other-account-id")

        XCTAssertFalse(viewModel.isEditable)

        await viewModel.load()

        XCTAssertEqual(viewModel.state, .loaded(Profile(displayName: "Museum Owner", avatarID: "")))
        XCTAssertEqual(service.receivedAccountIDs, ["other-account-id"])
    }

    func test_load_failure_setsFailedState() async {
        let service = FakeProfileService()
        service.result = .failure(IdentityAPIClientError.server(statusCode: 401, message: "authentication required"))
        let viewModel = ProfileViewModel(profileService: service, accessToken: "token-3")

        await viewModel.load()

        guard case .failed = viewModel.state else {
            XCTFail("expected .failed state, got \(viewModel.state)")
            return
        }
    }

    func test_save_onOwnProfile_updatesProfile() async {
        let service = FakeProfileService()
        service.result = .success(Profile(displayName: "New Name", avatarID: ""))
        let viewModel = ProfileViewModel(profileService: service, accessToken: "token-4")

        await viewModel.save(displayName: "New Name")

        XCTAssertEqual(viewModel.state, .loaded(Profile(displayName: "New Name", avatarID: "")))
        XCTAssertEqual(service.receivedDisplayNames, ["New Name"])
        XCTAssertEqual(service.receivedAvatarIDs, [nil], "a display-name save must not touch avatar_id")
    }

    func test_save_onOtherAccountProfile_isANoOp() async {
        let service = FakeProfileService()
        let viewModel = ProfileViewModel(profileService: service, accessToken: "token-5", accountID: "other-account-id")

        await viewModel.save(displayName: "Should Not Persist")

        XCTAssertTrue(service.receivedDisplayNames.isEmpty, "a read-only (visitor) profile must never call updateOwnProfile")
    }
}
