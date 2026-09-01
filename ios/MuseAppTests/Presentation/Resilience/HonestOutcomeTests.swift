import XCTest
@testable import MuseApp

final class HonestOutcomeTests: XCTestCase {

    // MARK: - The primitive

    func test_onlyANoPathFailureGuaranteesTheRequestWasNotDelivered() {
        for code in [URLError.Code.notConnectedToInternet, .dataNotAllowed,
                     .internationalRoamingOff, .callIsActive] {
            XCTAssertTrue(NetworkResilience.requestCertainlyNotDelivered(URLError(code)), "\(code)")
        }
        for code in [URLError.Code.timedOut, .networkConnectionLost,
                     .cannotConnectToHost, .secureConnectionFailed] {
            XCTAssertFalse(NetworkResilience.requestCertainlyNotDelivered(URLError(code)),
                           "\(code) may have been delivered with only the reply lost")
        }
        XCTAssertEqual(NetworkResilience.classify(URLError(.networkConnectionLost)), .offline)
        XCTAssertFalse(NetworkResilience.requestCertainlyNotDelivered(URLError(.networkConnectionLost)))
    }

    func test_serverRefusalsKeepTheDefiniteSentence() {
        let refusals: [Error] = [
            IdentityAPIClientError.server(statusCode: 400, message: nil),
            IdentityAPIClientError.server(statusCode: 409, message: nil),
            PhotoAPIError(statusCode: 404, message: nil, code: "photo_not_in_room", assetID: nil),
            CollectionAPIError(statusCode: 400, code: "unknown_music_track", message: nil),
        ]
        for refusal in refusals {
            XCTAssertEqual(
                NetworkFailureCopy.mutationOutcome(for: refusal, certainlyUnchanged: "definite", possiblyApplied: "unknown"),
                "definite", "\(refusal)")
        }
    }

    func test_mutationOutcomePicksTheHonestSentence() {
        XCTAssertEqual(
            NetworkFailureCopy.mutationOutcome(
                for: URLError(.notConnectedToInternet), certainlyUnchanged: "definite", possiblyApplied: "unknown"),
            "definite")
        XCTAssertEqual(
            NetworkFailureCopy.mutationOutcome(
                for: URLError(.timedOut), certainlyUnchanged: "definite", possiblyApplied: "unknown"),
            "unknown")
    }

    func test_theUnknownOutcomeSentencePointsAtReloadingNotRepeating() {
        let tail = NetworkFailureCopy.outcomeUnknownTail
        XCTAssertTrue(tail.contains("reload"), tail)
        XCTAssertFalse(tail.lowercased().contains("try again"), tail)
        XCTAssertTrue(tail.contains("may or may not"), tail)
    }

    // MARK: - Room content edits

    @MainActor
    func test_reorderFailure_distinguishesCertainFromUncertain() {
        let certainReorder = RoomContentEditCoordinator.failedTransportMessage
        XCTAssertTrue(certainReorder.contains("unchanged"))
        let uncertain = RoomContentEditCoordinator.failedUncertainMessage
        XCTAssertFalse(uncertain.contains("unchanged"),
                       "a reorder that may have committed must not be described as having changed nothing: \(uncertain)")
        XCTAssertTrue(uncertain.contains("reload"), uncertain)
    }

    @MainActor
    func test_replacementOutcomeUnknown_hasItsOwnMessage() {
        let unknown = RoomContentEditCoordinator.replacementFailureMessage(for: .transportOutcomeUnknown)
        let certain = RoomContentEditCoordinator.replacementFailureMessage(for: .transport)
        XCTAssertNotEqual(unknown, certain)
        XCTAssertTrue(certain.contains("unchanged"))
        XCTAssertFalse(unknown.contains("unchanged"), unknown)
        XCTAssertTrue(RoomContentEditCoordinator.replacementFailureMessage(for: .transferFailed).contains("unchanged"))
    }

    // MARK: - Sharing

    @MainActor
    func test_linkRegeneration_doesNotPromiseAnUnchangedLinkOnATimeout() async {
        let offline = await sharingFailureMessage(URLError(.notConnectedToInternet))
        let timedOut = await sharingFailureMessage(URLError(.timedOut))
        XCTAssertTrue(offline.contains("unchanged"), offline)
        XCTAssertFalse(timedOut.contains("unchanged"),
                       "a regenerate that timed out may have rotated the link: \(timedOut)")
        XCTAssertTrue(timedOut.contains("reload"), timedOut)
    }

    @MainActor
    private func sharingFailureMessage(_ error: Error) async -> String {
        let service = FailingShareLinkService(error: error)
        let viewModel = MuseumSharingViewModel(shareLinkService: service, accessToken: "t")
        let outcome = await viewModel.regenerateLink()
        guard case .failed(let message) = outcome else { return "<not a failure: \(outcome)>" }
        return message
    }
}

final class FailingShareLinkService: ShareLinkServicing, @unchecked Sendable {
    private let error: Error
    init(error: Error) { self.error = error }

    func ensureShareLink(accessToken: String) async throws -> MuseumShareLink { throw error }
    func currentShareLink(accessToken: String) async throws -> MuseumShareLink? { throw error }
    func regenerateShareLink(accessToken: String) async throws -> MuseumShareLink { throw error }
    func preview(code: String) async throws -> ShareLinkPreview { throw error }
    func sharedMuseum(accessToken: String, code: String) async throws -> SharedMuseumContent { throw error }
    func sharedRoom(accessToken: String, code: String, roomID: String) async throws -> SharedRoomContent { throw error }
    func sharedRoomPhotoURLs(accessToken: String, code: String, roomID: String) async throws -> [PhotoDownloadTicket] { throw error }
}
