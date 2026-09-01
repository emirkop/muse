import Foundation

@MainActor
public final class ProfileViewModel {
    public enum State: Equatable {
        case loading
        case loaded(Profile)
        case failed(message: String)
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public let isEditable: Bool

    private let profileService: ProfileServicing
    private let accessToken: String
    private let refresh = RefreshCoordination()

    public private(set) var refreshFailureNotice: String?
    private let accountID: String?

    public init(profileService: ProfileServicing, accessToken: String, accountID: String? = nil) {
        self.profileService = profileService
        self.accessToken = accessToken
        self.accountID = accountID
        self.isEditable = accountID == nil
    }

    public func load() async {
        let token = refresh.begin()
        if !hasProfile { state = .loading }
        do {
            let profile = try await fetchProfile()
            guard refresh.isCurrent(token) else { return }
            refreshFailureNotice = nil
            state = .loaded(profile)
        } catch {
            guard refresh.isCurrent(token) else { return }
            if hasProfile {
                refreshFailureNotice = RefreshFailureNotice.message(for: error)
                onStateChange?(state)
            } else {
                refreshFailureNotice = nil
                state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: genericFailureMessage))
            }
        }
    }

    private var hasProfile: Bool {
        if case .loaded = state { return true }
        return false
    }

    public func save(displayName: String) async {
        guard isEditable else { return }
        state = .loading
        do {
            state = .loaded(try await profileService.updateOwnProfile(accessToken: accessToken, displayName: displayName, avatarID: nil))
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: "Couldn't save your profile. Please try again."))
        }
    }

    private func fetchProfile() async throws -> Profile {
        if let accountID {
            return try await profileService.fetchProfile(accessToken: accessToken, accountID: accountID)
        }
        return try await profileService.fetchOwnProfile(accessToken: accessToken)
    }

    private var genericFailureMessage: String { "Couldn't load profile. Please try again." }
}
