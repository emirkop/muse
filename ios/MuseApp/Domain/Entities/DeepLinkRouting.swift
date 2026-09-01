import Foundation

public enum DeepLinkRouting {
    public enum Destination: Equatable, Sendable {
        case sharedMuseumLanding(code: String)
        case sharedCollectionRoomLanding(code: String)
        case accountCreation
        case mainHub
    }

    public static func destinationAfterAuthentication(pendingShareLink: MuseShareLink?, isNewAccount: Bool) -> Destination {
        if isNewAccount { return .accountCreation }
        return destinationAfterOnboarding(pendingShareLink: pendingShareLink)
    }

    public static func destinationAfterOnboarding(pendingShareLink: MuseShareLink?) -> Destination {
        switch pendingShareLink {
        case .museum(let code)?: return .sharedMuseumLanding(code: code)
        case .collectionRoom(let code)?: return .sharedCollectionRoomLanding(code: code)
        case nil: return .mainHub
        }
    }

    public static func destinationAfterAuthentication(pendingShareLinkCode: String?, isNewAccount: Bool) -> Destination {
        destinationAfterAuthentication(pendingShareLink: pendingShareLinkCode.map(MuseShareLink.museum), isNewAccount: isNewAccount)
    }

    public static func destinationAfterOnboarding(pendingShareLinkCode: String?) -> Destination {
        destinationAfterOnboarding(pendingShareLink: pendingShareLinkCode.map(MuseShareLink.museum))
    }

    public enum SharedMuseumEntry: Equatable, Sendable {
        case ownMuseum
        case visitor
    }

    public static func sharedMuseumEntry(sharedMuseumID: String, ownMuseumID: String?) -> SharedMuseumEntry {
        ownMuseumID == sharedMuseumID ? .ownMuseum : .visitor
    }

    public static func requiresAvatarSelection(avatarID: String?) -> Bool {
        guard let avatarID else { return true }
        return avatarID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}
