import Foundation

@MainActor
public final class CollectionShareLinkLandingViewModel {
    public enum Access: Equatable {
        case signedOut
        case signedIn(accessToken: String)
    }

    public enum State: Equatable {
        case ready
        case unavailable
        case failed(message: String)
    }

    public enum EnterOutcome: Equatable {
        case needsAuthentication
        case entered(SharedCollectionRoomContent)
        case unavailable
        case failed(message: String)
    }

    public private(set) var state: State = .ready {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public let code: String
    public let access: Access
    private let sharedRooms: SharedCollectionRoomReading

    public init(code: String, access: Access, sharedRooms: SharedCollectionRoomReading) {
        self.code = code
        self.access = access
        self.sharedRooms = sharedRooms
    }

    public var isSignedIn: Bool {
        if case .signedIn = access { return true }
        return false
    }

    public var primaryActionTitle: String {
        isSignedIn ? "Enter Collection Room" : "Sign in to visit"
    }

    public static let heading = "You've been invited to a Collection Room."
    public static let detail = "Sign in to Muse to see it. Only this Collection Room is shared — nothing else."

    public func enter() async -> EnterOutcome {
        guard case .signedIn(let accessToken) = access else {
            return .needsAuthentication
        }
        do {
            return .entered(try await sharedRooms.sharedCollectionRoom(accessToken: accessToken, code: code))
        } catch let error as CollectionAPIError where error.statusCode == 404 {
            state = .unavailable
            return .unavailable
        } catch {
            return .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't enter right now. Please try again."))
        }
    }

    public func reset() {
        state = .ready
    }
}
