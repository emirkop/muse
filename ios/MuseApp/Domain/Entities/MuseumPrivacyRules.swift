import Foundation

public enum RoomVisitorVisibility: Equatable, Sendable {
    case visible
    case hiddenByRoom
    case hiddenByMuseum
}

public enum MuseumPrivacyRules {
    public static func visitorVisibility(museum: MusePrivacy, room: MusePrivacy) -> RoomVisitorVisibility {
        guard museum == .public else { return .hiddenByMuseum }
        return room == .public ? .visible : .hiddenByRoom
    }

    public static func museumIsReachableByVisitors(_ privacy: MusePrivacy) -> Bool {
        privacy == .public
    }

    public static func museumChangeNeedsExposureConfirmation(
        from current: MusePrivacy,
        to target: MusePrivacy
    ) -> Bool {
        current == .private && target == .public
    }

    public static func roomChangeNeedsExposureConfirmation(
        museum: MusePrivacy,
        from current: MusePrivacy,
        to target: MusePrivacy
    ) -> Bool {
        museum == .public && current == .private && target == .public
    }

    public static func roomsExposedByMakingMuseumPublic(_ rooms: [Room]) -> Int {
        rooms.filter { $0.privacy == .public }.count
    }
}
