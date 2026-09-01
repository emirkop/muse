import Foundation

public struct MuseumShareLink: Equatable, Sendable {
    public let code: String
    public let url: URL
    public let createdAt: Date

    public init(code: String, url: URL, createdAt: Date) {
        self.code = code
        self.url = url
        self.createdAt = createdAt
    }
}

public struct ShareLinkPreview: Equatable, Sendable {
    public let code: String
    public let styleID: String
    public let ownerAvatarID: String

    public init(code: String, styleID: String, ownerAvatarID: String) {
        self.code = code
        self.styleID = styleID
        self.ownerAvatarID = ownerAvatarID
    }
}

public struct SharedMuseumContent: Equatable, Sendable {
    public let museumID: String
    public let styleID: String
    public let rooms: [SharedRoomSummary]

    public init(museumID: String, styleID: String, rooms: [SharedRoomSummary]) {
        self.museumID = museumID
        self.styleID = styleID
        self.rooms = rooms
    }
}

public struct SharedRoomContent: Equatable, Sendable {
    public let id: String
    public let name: String
    public let variantID: String
    public let musicTrackID: String?
    public let photoSlots: [PhotoSlotAssignment]
    public let sculptures: [SculptureInstance]

    public init(
        id: String,
        name: String,
        variantID: String,
        musicTrackID: String? = nil,
        photoSlots: [PhotoSlotAssignment],
        sculptures: [SculptureInstance]
    ) {
        self.id = id
        self.name = name
        self.variantID = variantID
        self.musicTrackID = musicTrackID
        self.photoSlots = photoSlots
        self.sculptures = sculptures
    }

    public var room: Room {
        Room(
            id: id,
            name: name,
            variantID: variantID,
            privacy: .public,
            musicTrackID: musicTrackID,
            photoSlots: photoSlots,
            sculptures: sculptures
        )
    }
}

public struct SharedRoomSummary: Equatable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let variantID: String

    public init(id: String, name: String, variantID: String) {
        self.id = id
        self.name = name
        self.variantID = variantID
    }
}

public enum MuseShareLink: Equatable, Sendable {
    case museum(code: String)
    case collectionRoom(code: String)

    public var code: String {
        switch self {
        case .museum(let code), .collectionRoom(let code):
            return code
        }
    }
}

public enum MuseShareLinkURL {
    public static func parse(_ url: URL, acceptedHosts: Set<String>) -> MuseShareLink? {
        guard let host = url.host?.lowercased(), acceptedHosts.contains(host) else { return nil }
        let scheme = url.scheme?.lowercased()
        let loopback = host == "localhost" || host == "127.0.0.1"
        guard scheme == "https" || (scheme == "http" && loopback) else { return nil }

        let segments = url.pathComponents.filter { $0 != "/" }
        guard segments.count == 2 else { return nil }
        let code = segments[1]
        guard isPlausibleCode(code) else { return nil }
        switch segments[0] {
        case "m": return .museum(code: code)
        case "c": return .collectionRoom(code: code)
        default: return nil
        }
    }

    public static func code(from url: URL, acceptedHosts: Set<String>) -> String? {
        guard case .museum(let code)? = parse(url, acceptedHosts: acceptedHosts) else { return nil }
        return code
    }

    public static func isPlausibleCode(_ code: String) -> Bool {
        guard code.utf8.count == 22 else { return false }
        return code.utf8.allSatisfy { c in
            (c >= 0x61 && c <= 0x7A) || (c >= 0x41 && c <= 0x5A) || (c >= 0x30 && c <= 0x39) || c == 0x2D || c == 0x5F
        }
    }
}
