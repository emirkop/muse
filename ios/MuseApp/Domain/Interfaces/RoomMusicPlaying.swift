import Foundation

@MainActor
public protocol RoomMusicPlaying: AnyObject, Sendable {
    func start(url: URL, trackID: String)

    func stop()

    func setMuted(_ muted: Bool)

    var currentTrackID: String? { get }
}

public protocol MusicCatalogServicing: Sendable {
    func fetchMusicTracks(accessToken: String) async throws -> [MusicTrack]

    func musicAudioURL(accessToken: String, trackID: String) async throws -> MusicAudioURL
}
