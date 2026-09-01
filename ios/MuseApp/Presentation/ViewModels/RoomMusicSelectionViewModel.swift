import Foundation

@MainActor
public final class RoomMusicSelectionViewModel {
    public enum State: Equatable {
        case loading
        case catalogEmpty
        case loaded(tracks: [MusicTrack], assignedTrackID: String?)
        case failed(message: String, tracks: [MusicTrack])
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public private(set) var assignedTrackID: String?

    private let musicCatalog: any MusicCatalogServicing
    private let assignment: any MusicAssigning
    private let accessToken: String

    public init(
        assignedTrackID: String?,
        assignment: any MusicAssigning,
        musicCatalog: any MusicCatalogServicing,
        accessToken: String
    ) {
        self.assignedTrackID = assignedTrackID
        self.assignment = assignment
        self.musicCatalog = musicCatalog
        self.accessToken = accessToken
    }

    public func load() async {
        state = .loading
        do {
            let tracks = try await musicCatalog.fetchMusicTracks(accessToken: accessToken)
            state = tracks.isEmpty ? .catalogEmpty : .loaded(tracks: tracks, assignedTrackID: assignedTrackID)
        } catch {
            state = .failed(
                message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load the music library. Please try again."),
                tracks: currentTracks
            )
        }
    }

    public func assign(trackID: String) async {
        do {
            assignedTrackID = try await assignment.assignMusic(trackID: trackID)
            state = .loaded(tracks: currentTracks, assignedTrackID: assignedTrackID)
        } catch {
            state = .failed(
                message: NetworkFailureCopy.mutationOutcome(
                    for: error,
                    certainlyUnchanged: "Couldn't set that track. Your Room's music is unchanged.",
                    possiblyApplied: "Couldn't confirm the change. \(NetworkFailureCopy.outcomeUnknownTail)"
                ),
                tracks: currentTracks
            )
        }
    }

    public func removeMusic() async {
        do {
            assignedTrackID = try await assignment.removeMusic()
            state = .loaded(tracks: currentTracks, assignedTrackID: assignedTrackID)
        } catch {
            state = .failed(
                message: NetworkFailureCopy.mutationOutcome(
                    for: error,
                    certainlyUnchanged: "Couldn't remove the music. Your Room's music is unchanged.",
                    possiblyApplied: "Couldn't confirm the change. \(NetworkFailureCopy.outcomeUnknownTail)"
                ),
                tracks: currentTracks
            )
        }
    }

    private var currentTracks: [MusicTrack] {
        switch state {
        case .loaded(let tracks, _): return tracks
        case .failed(_, let tracks): return tracks
        case .loading, .catalogEmpty: return []
        }
    }

    // MARK: - Copy

    public var catalogEmptyMessage: String {
        "Muse doesn't have any music to offer yet. Room music will come from a curated, "
            + "properly licensed library, and none has been added so far."
    }

    public func subtitle(for track: MusicTrack) -> String {
        var parts: [String] = []
        if track.durationSeconds > 0 {
            parts.append("\(track.durationSeconds / 60):" + String(format: "%02d", track.durationSeconds % 60))
        }
        if !track.attribution.isEmpty {
            parts.append(track.attribution)
        }
        switch track.licensing {
        case .licensed:
            break
        case .devTest:
            parts.append("Development audio — not licensed content")
        case .unknown:
            parts.append("Licensing unconfirmed")
        }
        return parts.joined(separator: " · ")
    }
}
