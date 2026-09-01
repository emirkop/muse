import Foundation

@MainActor
public final class AvatarSelectionViewModel {
    public enum State: Equatable {
        case idle
        case saving
        case saved(avatarID: String)
        case failed(message: String)
    }

    public private(set) var state: State = .idle {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let profileService: ProfileServicing
    private let accessToken: String

    public init(profileService: ProfileServicing, accessToken: String) {
        self.profileService = profileService
        self.accessToken = accessToken
    }

    public func selectAvatar(_ avatarID: String) async {
        state = .saving
        do {
            let profile = try await profileService.updateOwnProfile(accessToken: accessToken, displayName: nil, avatarID: avatarID)
            state = .saved(avatarID: profile.avatarID)
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: "Couldn't save your avatar. Please try again."))
        }
    }
}
