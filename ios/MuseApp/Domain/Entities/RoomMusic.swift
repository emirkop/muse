import Foundation

public struct MusicTrack: Equatable, Sendable, Identifiable {
    public enum Licensing: String, Equatable, Sendable {
        case devTest = "dev_test"
        case licensed
        case unknown
    }

    public let id: String
    public let displayName: String
    public let attribution: String
    public let licensing: Licensing
    public let durationSeconds: Int

    public init(id: String, displayName: String, attribution: String, licensing: Licensing, durationSeconds: Int) {
        self.id = id
        self.displayName = displayName
        self.attribution = attribution
        self.licensing = licensing
        self.durationSeconds = durationSeconds
    }

    public var isLicensed: Bool { licensing == .licensed }
}

public struct MusicAudioURL: Equatable, Sendable {
    public let url: URL
    public let expiresAt: Date

    public init(url: URL, expiresAt: Date) {
        self.url = url
        self.expiresAt = expiresAt
    }

    public func isValid(at moment: Date = Date()) -> Bool { moment < expiresAt }
}

public struct RoomMusicState: Equatable, Sendable {
    public let trackID: String?
    public private(set) var isMutedLocally: Bool

    public init(trackID: String?, isMutedLocally: Bool = false) {
        self.trackID = trackID
        self.isMutedLocally = isMutedLocally
    }

    public var hasTrack: Bool { trackID != nil }

    public var shouldPlay: Bool { hasTrack && !isMutedLocally }

    public var offersToggle: Bool { hasTrack }

    public func togglingMute() -> RoomMusicState {
        RoomMusicState(trackID: trackID, isMutedLocally: !isMutedLocally)
    }

    public func settingMuted(_ muted: Bool) -> RoomMusicState {
        RoomMusicState(trackID: trackID, isMutedLocally: muted)
    }

    public var toggleTitle: String { isMutedLocally ? "Unmute Music" : "Mute Music" }
}
