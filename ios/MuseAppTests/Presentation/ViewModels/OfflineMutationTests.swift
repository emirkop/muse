import XCTest
@testable import MuseApp

@MainActor
final class OfflineMutationTests: XCTestCase {

    // MARK: - Case 3 — mutations fail clearly offline and are not queued

    func test_offlineMutation_failsWithConnectionRequiredMessaging() async {
        let service = FakeProfileService()
        service.result = .failure(URLError(.notConnectedToInternet))
        let viewModel = AvatarSelectionViewModel(profileService: service, accessToken: "t")

        await viewModel.selectAvatar("avatar_2")

        guard case .failed(let message) = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
        XCTAssertTrue(message.lowercased().contains("offline"), message)
        XCTAssertTrue(message.contains("connection"), message)
        XCTAssertTrue(message.contains("wasn't saved"), message)
    }

    func test_timedOutMutation_makesNoClaimAboutWhetherItSaved() async {
        let service = FakeProfileService()
        service.result = .failure(URLError(.timedOut))
        let viewModel = AvatarSelectionViewModel(profileService: service, accessToken: "t")

        await viewModel.selectAvatar("avatar_2")

        guard case .failed(let message) = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
        XCTAssertFalse(message.contains("wasn't saved"), message)
    }

    func test_offlineMutation_isAttemptedExactlyOnceAndNotQueued() async {
        let service = FakeProfileService()
        service.result = .failure(URLError(.notConnectedToInternet))
        let viewModel = AvatarSelectionViewModel(profileService: service, accessToken: "t")

        await viewModel.selectAvatar("avatar_2")
        XCTAssertEqual(service.receivedAvatarIDs, ["avatar_2"])

        service.result = .success(Profile(displayName: "Ada", avatarID: "avatar_2"))
        try? await Task.sleep(nanoseconds: 50_000_000)
        XCTAssertEqual(service.receivedAvatarIDs, ["avatar_2"],
                       "an offline mutation must not be replayed on its own — builds no write queue")
        guard case .failed = viewModel.state else {
            return XCTFail("the state must stay failed until the user retries, got \(viewModel.state)")
        }
    }

    // MARK: - Case 4 — reconnect and retry, with no restart

    func test_retryAfterReconnecting_succeedsOnTheSameViewModel() async {
        let service = FakeProfileService()
        service.result = .failure(URLError(.notConnectedToInternet))
        let viewModel = AvatarSelectionViewModel(profileService: service, accessToken: "t")

        await viewModel.selectAvatar("avatar_4")
        guard case .failed = viewModel.state else { return XCTFail("expected an offline failure first") }

        service.result = .success(Profile(displayName: "Ada", avatarID: "avatar_4"))
        await viewModel.selectAvatar("avatar_4")

        XCTAssertEqual(viewModel.state, .saved(avatarID: "avatar_4"))
        XCTAssertEqual(service.receivedAvatarIDs, ["avatar_4", "avatar_4"],
                       "exactly one attempt per user action, before and after")
    }

    // MARK: - Case 8, at the view-model layer

    func test_serverRefusal_keepsTheScreensOwnMessage() async {
        let service = FakeProfileService()
        service.result = .failure(IdentityAPIClientError.server(statusCode: 400, message: "invalid avatar_id"))
        let viewModel = AvatarSelectionViewModel(profileService: service, accessToken: "t")

        await viewModel.selectAvatar("avatar_9")

        guard case .failed(let message) = viewModel.state else {
            return XCTFail("expected .failed, got \(viewModel.state)")
        }
        XCTAssertEqual(message, "Couldn't save your avatar. Please try again.")
        XCTAssertFalse(message.lowercased().contains("offline"))
    }

    func test_readAndMutationOfflineCopyDiffer() async {
        let museum = FakeMuseumService()
        museum.roomsResult = .failure(URLError(.notConnectedToInternet))
        let readModel = RoomListViewModel(museumService: museum, accessToken: "t")
        await readModel.load()

        guard case .failed(let readMessage) = readModel.state else {
            return XCTFail("expected a read failure, got \(readModel.state)")
        }

        let profile = FakeProfileService()
        profile.result = .failure(URLError(.notConnectedToInternet))
        let mutationModel = AvatarSelectionViewModel(profileService: profile, accessToken: "t")
        await mutationModel.selectAvatar("avatar_1")
        guard case .failed(let mutationMessage) = mutationModel.state else {
            return XCTFail("expected a mutation failure, got \(mutationModel.state)")
        }

        XCTAssertTrue(readMessage.lowercased().contains("offline"), readMessage)
        XCTAssertNotEqual(readMessage, mutationMessage)
        XCTAssertFalse(readMessage.contains("wasn't saved"),
                       "a read has nothing to save; only a mutation makes that claim")
        XCTAssertTrue(readMessage.contains("restart"), readMessage)
    }
}
