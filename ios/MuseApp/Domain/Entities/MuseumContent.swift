import Foundation

public enum MusePrivacy: String, Equatable, Sendable, CaseIterable {
    case `public`
    case `private`
}

public struct Museum: Equatable, Sendable {
    public let id: String
    public let styleID: String
    public let privacy: MusePrivacy

    public init(id: String, styleID: String, privacy: MusePrivacy) {
        self.id = id
        self.styleID = styleID
        self.privacy = privacy
    }
}

public struct Room: Equatable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let variantID: String
    public let privacy: MusePrivacy
    public let musicTrackID: String?
    public let photoSlots: [PhotoSlotAssignment]
    public let sculptures: [SculptureInstance]

    public init(
        id: String,
        name: String,
        variantID: String,
        privacy: MusePrivacy,
        musicTrackID: String? = nil,
        photoSlots: [PhotoSlotAssignment] = [],
        sculptures: [SculptureInstance] = []
    ) {
        self.id = id
        self.name = name
        self.variantID = variantID
        self.privacy = privacy
        self.musicTrackID = musicTrackID
        self.photoSlots = photoSlots
        self.sculptures = sculptures
    }

    public var hasMusic: Bool { musicTrackID != nil }

    public static let maxPhotos = 28
    public static let maxSculptures = 3

    public var hasCapacityForPhoto: Bool { photoSlots.count < Self.maxPhotos }
    public var hasCapacityForSculpture: Bool { sculptures.count < Self.maxSculptures }
}

public struct PhotoSlotAssignment: Equatable, Sendable {
    public let slotIndex: Int
    public let photoAssetID: String
    public let caption: String

    public init(slotIndex: Int, photoAssetID: String, caption: String) {
        self.slotIndex = slotIndex
        self.photoAssetID = photoAssetID
        self.caption = caption
    }
}

public struct SculptureInstance: Equatable, Sendable {
    public let slotIndex: Int
    public let catalogID: String

    public init(slotIndex: Int, catalogID: String) {
        self.slotIndex = slotIndex
        self.catalogID = catalogID
    }
}
