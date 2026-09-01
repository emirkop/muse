import UIKit

@MainActor
final class MusicToggleBarButton {
    private let session: RoomMusicSession
    private let button: UIBarButtonItem

    init(session: RoomMusicSession) {
        self.session = session
        self.button = UIBarButtonItem(image: nil, style: .plain, target: nil, action: nil)
        button.target = self
        button.action = #selector(handleToggle)
        button.accessibilityIdentifier = "room-music-toggle"
        session.onStateChange = { [weak self] _ in self?.render() }
        render()
    }

    var item: UIBarButtonItem? {
        session.state.offersToggle ? button : nil
    }

    func render() {
        button.image = UIImage(systemName: session.state.isMutedLocally ? "speaker.slash.fill" : "speaker.wave.2.fill")
        button.accessibilityLabel = session.state.toggleTitle
    }

    @objc private func handleToggle() {
        session.toggleMute()
        render()
    }
}
