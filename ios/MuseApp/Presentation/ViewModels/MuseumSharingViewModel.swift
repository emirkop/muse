import Foundation

@MainActor
public final class MuseumSharingViewModel {
    public enum Outcome: Equatable {
        case museumIsPrivate
        case link(MuseumShareLink)
        case failed(message: String)
    }

    private let shareLinkService: ShareLinkServicing
    private let accessToken: String

    public init(shareLinkService: ShareLinkServicing, accessToken: String) {
        self.shareLinkService = shareLinkService
        self.accessToken = accessToken
    }

    public func shareLink(for museum: Museum) async -> Outcome {
        guard MuseumPrivacyRules.museumIsReachableByVisitors(museum.privacy) else {
            return .museumIsPrivate
        }
        do {
            return .link(try await shareLinkService.ensureShareLink(accessToken: accessToken))
        } catch {
            return .failed(message: NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: "Couldn't get your Museum's link. Please try again."))
        }
    }

    public func regenerateLink() async -> Outcome {
        do {
            return .link(try await shareLinkService.regenerateShareLink(accessToken: accessToken))
        } catch {
            return .failed(message: NetworkFailureCopy.mutationOutcome(
                for: error,
                certainlyUnchanged: "Couldn't create a new link. Your current link is unchanged.",
                possiblyApplied: "Couldn't confirm the new link. \(NetworkFailureCopy.outcomeUnknownTail)"
            ))
        }
    }
}
