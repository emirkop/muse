import Foundation

public struct RoomRuntimeContent: Sendable {
    public enum Geometry: Equatable, Sendable {
        case verificationFixture
        case variantBundle(RoomVariantGeometry)
    }

    public let roomID: String
    public let accessToken: String
    public let geometry: Geometry
    public let viewerRole: RoomViewerRole
    public let room: Room
    public let slotTable: RoomVariantSlotTable
    public let placements: [ResolvedPhotoPlacement]
    public let textures: any RoomPhotoTextureProviding
    public let photoService: (any RoomPhotoServicing)?
    public let roomService: (any MuseumServicing)?
    public let photoReplacer: (any RoomPhotoReplacing)?
    public let sculptureModels: (any SculptureModelProviding)?
    public let catalogService: (any CatalogServicing)?
    public let musicCatalog: (any MusicCatalogServicing)?
    public let musicPlayer: (any RoomMusicPlaying)?
    public let bundleRetention: (any AssetBundleRetaining)?

    public init(
        roomID: String,
        accessToken: String,
        geometry: Geometry,
        viewerRole: RoomViewerRole,
        room: Room,
        slotTable: RoomVariantSlotTable,
        placements: [ResolvedPhotoPlacement],
        textures: any RoomPhotoTextureProviding,
        photoService: (any RoomPhotoServicing)? = nil,
        roomService: (any MuseumServicing)? = nil,
        photoReplacer: (any RoomPhotoReplacing)? = nil,
        sculptureModels: (any SculptureModelProviding)? = nil,
        catalogService: (any CatalogServicing)? = nil,
        musicCatalog: (any MusicCatalogServicing)? = nil,
        musicPlayer: (any RoomMusicPlaying)? = nil,
        bundleRetention: (any AssetBundleRetaining)? = nil
    ) {
        self.roomID = roomID
        self.accessToken = accessToken
        self.geometry = geometry
        self.viewerRole = viewerRole
        self.room = room
        self.slotTable = slotTable
        self.placements = placements
        self.textures = textures
        self.photoService = photoService
        self.roomService = roomService
        self.photoReplacer = photoReplacer
        self.sculptureModels = sculptureModels
        self.catalogService = catalogService
        self.musicCatalog = musicCatalog
        self.musicPlayer = musicPlayer
        self.bundleRetention = bundleRetention
    }

    public var supportsMusicPlayback: Bool {
        room.hasMusic && musicCatalog != nil && musicPlayer != nil
    }

    public var supportsOwnerEditing: Bool {
        viewerRole == .owner && photoService != nil && roomService != nil
    }

    public var supportsPhotoReplacement: Bool {
        supportsOwnerEditing && photoReplacer != nil
    }

    public var supportsSculptureEditing: Bool {
        supportsOwnerEditing && catalogService != nil
    }

    public var supportsMusicAssignment: Bool {
        supportsOwnerEditing && musicCatalog != nil
    }
}

public struct RoomVariantGeometry: Equatable, Sendable {
    public let variantID: String
    public let identity: AssetBundleIdentity
    public let format: String
    public let fileURL: URL
    public let entry: MuseumCameraSubject

    public init(variantID: String, identity: AssetBundleIdentity, format: String, fileURL: URL, entry: MuseumCameraSubject) {
        self.variantID = variantID
        self.identity = identity
        self.format = format
        self.fileURL = fileURL
        self.entry = entry
    }
}

public enum RoomViewerRole: Equatable, Sendable {
    case owner
    case visitor
}
