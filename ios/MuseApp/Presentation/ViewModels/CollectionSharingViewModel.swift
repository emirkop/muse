import Foundation

@MainActor
public final class CollectionSharingViewModel {
    public enum Outcome: Equatable {
        case link(CollectionRoomShareLink)
        case failed(message: String)
    }

    public enum RevokeOutcome: Equatable {
        case revoked
        case failed(message: String)
    }

    private let shareLinks: CollectionShareLinkServicing
    private let accessToken: String
    public let collectionRoomID: String

    public init(shareLinks: CollectionShareLinkServicing, accessToken: String, collectionRoomID: String) {
        self.shareLinks = shareLinks
        self.accessToken = accessToken
        self.collectionRoomID = collectionRoomID
    }

    public func shareLink() async -> Outcome {
        do {
            return .link(try await shareLinks.ensureCollectionShareLink(accessToken: accessToken, collectionRoomID: collectionRoomID))
        } catch {
            return .failed(message: NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: "Couldn't get this Collection Room's link. Please try again."))
        }
    }

    public func regenerateLink() async -> Outcome {
        do {
            return .link(try await shareLinks.regenerateCollectionShareLink(accessToken: accessToken, collectionRoomID: collectionRoomID))
        } catch {
            return .failed(message: NetworkFailureCopy.mutationOutcome(
                for: error,
                certainlyUnchanged: "Couldn't create a new link. Your current link is unchanged.",
                possiblyApplied: "Couldn't confirm the new link. \(NetworkFailureCopy.outcomeUnknownTail)"
            ))
        }
    }

    public func stopSharing() async -> RevokeOutcome {
        do {
            try await shareLinks.revokeCollectionShareLink(accessToken: accessToken, collectionRoomID: collectionRoomID)
            return .revoked
        } catch {
            return .failed(message: NetworkFailureCopy.mutationOutcome(
                for: error,
                certainlyUnchanged: "Couldn't stop sharing. Your current link is unchanged.",
                possiblyApplied: "Couldn't confirm that sharing stopped. \(NetworkFailureCopy.outcomeUnknownTail)"
            ))
        }
    }
}
