import Foundation

public enum MuseumViewerRole: Equatable, Sendable, CaseIterable {
    case owner
    case visitor
}

public struct LobbyRoomCard: Equatable, Sendable, Identifiable {
    public let roomID: String
    public let name: String
    public let isMarkedPrivate: Bool

    public var id: String { roomID }

    public init(roomID: String, name: String, isMarkedPrivate: Bool) {
        self.roomID = roomID
        self.name = name
        self.isMarkedPrivate = isMarkedPrivate
    }

    public var signageText: String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? "Untitled Room" : trimmed
    }
}

public enum MuseumLobbyVisibility {
    public static func visibleCards(rooms: [Room], viewerRole: MuseumViewerRole) -> [LobbyRoomCard] {
        switch viewerRole {
        case .owner:
            return rooms.map {
                LobbyRoomCard(roomID: $0.id, name: $0.name, isMarkedPrivate: $0.privacy == .private)
            }
        case .visitor:
            return rooms
                .filter { $0.privacy == .public }
                .map { LobbyRoomCard(roomID: $0.id, name: $0.name, isMarkedPrivate: false) }
        }
    }
}
