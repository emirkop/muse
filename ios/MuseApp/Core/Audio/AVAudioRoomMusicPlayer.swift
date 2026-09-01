import AVFoundation
import Foundation

@MainActor
public final class AVAudioRoomMusicPlayer: RoomMusicPlaying {
    private var player: AVPlayer?
    private var loopObserver: NSObjectProtocol?
    private var loadedTrackID: String?
    private var isMuted = false

    public init() {}

    isolated deinit {
        if let loopObserver {
            NotificationCenter.default.removeObserver(loopObserver)
        }
    }

    public var currentTrackID: String? { loadedTrackID }

    public func start(url: URL, trackID: String) {
        if loadedTrackID == trackID, player != nil {
            applyMuteToPlayer()
            return
        }
        stop()

        configureSessionForAmbientPlayback()

        let item = AVPlayerItem(url: url)
        let player = AVPlayer(playerItem: item)
        player.volume = Self.ambientVolume
        player.actionAtItemEnd = .none
        self.player = player
        loadedTrackID = trackID

        loopObserver = NotificationCenter.default.addObserver(
            forName: AVPlayerItem.didPlayToEndTimeNotification,
            object: item,
            queue: .main
        ) { [weak player] _ in
            player?.seek(to: .zero)
            player?.play()
        }

        applyMuteToPlayer()
    }

    public func stop() {
        if let loopObserver {
            NotificationCenter.default.removeObserver(loopObserver)
            self.loopObserver = nil
        }
        player?.pause()
        player = nil
        loadedTrackID = nil
        deactivateSession()
    }

    public func setMuted(_ muted: Bool) {
        isMuted = muted
        applyMuteToPlayer()
    }

    private func applyMuteToPlayer() {
        guard let player else { return }
        if isMuted {
            player.pause()
        } else {
            player.play()
        }
    }

    // MARK: - Audio session

    private func configureSessionForAmbientPlayback() {
        let session = AVAudioSession.sharedInstance()
        do {
            try session.setCategory(.ambient, mode: .default, options: [])
            try session.setActive(true)
        } catch {
        }
    }

    private func deactivateSession() {
        do {
            try AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
        } catch {
        }
    }

    private static let ambientVolume: Float = 0.6
}
