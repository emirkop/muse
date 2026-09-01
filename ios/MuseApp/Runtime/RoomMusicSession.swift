import Foundation

@MainActor
public final class RoomMusicSession {
    public private(set) var state: RoomMusicState {
        didSet {
            guard state != oldValue else { return }
            onStateChange?(state)
        }
    }
    public var onStateChange: ((RoomMusicState) -> Void)?

    public private(set) var lastFailure: Failure?

    public enum Failure: Equatable, Sendable {
        case notAvailable
        case notFound
        case transport
    }

    private let catalog: any MusicCatalogServicing
    private let player: any RoomMusicPlaying
    private let accessToken: String
    private var generation = 0

    public init(
        trackID: String?,
        catalog: any MusicCatalogServicing,
        player: any RoomMusicPlaying,
        accessToken: String
    ) {
        self.state = RoomMusicState(trackID: trackID)
        self.catalog = catalog
        self.player = player
        self.accessToken = accessToken
    }

    public func enterRoom() async {
        guard state.hasTrack else { return }
        guard state.shouldPlay else { return }
        await resolveAndPlay()
    }

    public func toggleMute() {
        state = state.togglingMute()
        player.setMuted(state.isMutedLocally)
        if state.shouldPlay, player.currentTrackID == nil {
            Task { await resolveAndPlay() }
        }
    }

    public func roomTrackChanged(to trackID: String?) async {
        guard trackID != state.trackID else { return }
        generation += 1
        player.stop()
        state = RoomMusicState(trackID: trackID, isMutedLocally: state.isMutedLocally)
        lastFailure = nil
        guard state.shouldPlay else { return }
        await resolveAndPlay()
    }

    public func leaveRoom() {
        generation += 1
        player.stop()
    }

    // MARK: - Resolution

    private func resolveAndPlay() async {
        guard let trackID = state.trackID else { return }
        let attempt = generation
        do {
            let audio = try await catalog.musicAudioURL(accessToken: accessToken, trackID: trackID)
            guard attempt == generation, state.trackID == trackID, state.shouldPlay else { return }
            lastFailure = nil
            player.start(url: audio.url, trackID: trackID)
            player.setMuted(state.isMutedLocally)
        } catch IdentityAPIClientError.server(let statusCode, _) {
            guard attempt == generation else { return }
            lastFailure = statusCode == 404 ? .notFound : .notAvailable
        } catch {
            guard attempt == generation else { return }
            lastFailure = .transport
        }
    }
}
