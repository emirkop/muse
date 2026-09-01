import Foundation

@MainActor
public final class ShareLinkLandingViewModel {
    public enum Access: Equatable {
        case signedOut
        case signedIn(accessToken: String)
    }

    public enum State: Equatable {
        case loading
        case available(ShareLinkPreview)
        case unavailable
        case failed(message: String)
    }

    public enum EnterOutcome: Equatable {
        case needsAuthentication
        case entered(SharedMuseumContent)
        case unavailable
        case failed(message: String)
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public let code: String
    public let access: Access
    private let shareLinkService: ShareLinkServicing

    public init(code: String, access: Access, shareLinkService: ShareLinkServicing) {
        self.code = code
        self.access = access
        self.shareLinkService = shareLinkService
    }

    public func load() async {
        state = .loading
        do {
            state = .available(try await shareLinkService.preview(code: code))
        } catch IdentityAPIClientError.server(let statusCode, _) where statusCode == 404 {
            state = .unavailable
        } catch {
            state = .failed(message: NetworkFailureCopy.message(
                for: error, operation: .read,
                otherwise: "Couldn't open this link. Please try again."))
        }
    }

    public var isSignedIn: Bool {
        if case .signedIn = access { return true }
        return false
    }

    public var primaryActionTitle: String {
        isSignedIn ? "Enter Museum" : "Sign in to visit"
    }

    public static let heading = "You've been invited to a Museum."

    public func enter() async -> EnterOutcome {
        guard case .signedIn(let accessToken) = access else {
            return .needsAuthentication
        }
        do {
            return .entered(try await shareLinkService.sharedMuseum(accessToken: accessToken, code: code))
        } catch IdentityAPIClientError.server(let statusCode, _) where statusCode == 404 {
            state = .unavailable
            return .unavailable
        } catch {
            return .failed(message: NetworkFailureCopy.message(
                for: error, operation: .read,
                otherwise: "Couldn't enter right now. Please try again."))
        }
    }
}
