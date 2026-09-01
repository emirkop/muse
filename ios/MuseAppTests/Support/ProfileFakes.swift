import Foundation
@testable import MuseApp

final class FakeProfileService: ProfileServicing, @unchecked Sendable {
    var result: Result<Profile, Error> = .success(Profile(displayName: "", avatarID: ""))
    private(set) var receivedAccessTokens: [String] = []
    private(set) var receivedAccountIDs: [String] = []
    private(set) var receivedDisplayNames: [String?] = []
    private(set) var receivedAvatarIDs: [String?] = []

    func fetchOwnProfile(accessToken: String) async throws -> Profile {
        receivedAccessTokens.append(accessToken)
        return try result.get()
    }

    func updateOwnProfile(accessToken: String, displayName: String?, avatarID: String?) async throws -> Profile {
        receivedAccessTokens.append(accessToken)
        receivedDisplayNames.append(displayName)
        receivedAvatarIDs.append(avatarID)
        return try result.get()
    }

    func fetchProfile(accessToken: String, accountID: String) async throws -> Profile {
        receivedAccessTokens.append(accessToken)
        receivedAccountIDs.append(accountID)
        return try result.get()
    }
}
