import Foundation

public protocol ProfileServicing: Sendable {
    func fetchOwnProfile(accessToken: String) async throws -> Profile

    func updateOwnProfile(accessToken: String, displayName: String?, avatarID: String?) async throws -> Profile

    func fetchProfile(accessToken: String, accountID: String) async throws -> Profile
}
