import Foundation

@MainActor
public final class RoomEntryViewModel {
    public enum State: Equatable {
        case checking
        case downloading(fractionComplete: Double)
        case geometryReady(fractionComplete: Double)
        case designUnavailable(variantID: String, reason: RoomDesignUnavailableReason)
        case placementUnresolvable(RoomPlacementFailure)
        case ready

        var isTerminal: Bool {
            switch self {
            case .checking, .downloading, .geometryReady: return false
            case .designUnavailable, .placementUnresolvable, .ready: return true
            }
        }

        var fractionComplete: Double {
            switch self {
            case .downloading(let fraction), .geometryReady(let fraction): return fraction
            case .ready: return 1
            default: return 0
            }
        }
    }

    public static let defaultExtendedWaitThreshold: Duration = .milliseconds(1500)

    public private(set) var state: State = .checking {
        didSet {
            guard state != oldValue else { return }
            onStateChange?(state)
        }
    }
    public private(set) var showsExtendedWaitCopy = false {
        didSet {
            guard showsExtendedWaitCopy != oldValue else { return }
            onStateChange?(state)
        }
    }
    public var onStateChange: ((State) -> Void)?

    public let room: Room
    public let viewerRole: RoomViewerRole
    public private(set) var content: RoomRuntimeContent?

    private let design: any RoomDesignProviding
    private let textures: RoomPhotoTextureProviding
    private let accessToken: String
    private let extendedWaitThreshold: Duration
    private let photoService: (any RoomPhotoServicing)?
    private let roomService: (any MuseumServicing)?
    private let photoReplacer: (any RoomPhotoReplacing)?
    private let sculptureModels: (any SculptureModelProviding)?
    private let catalogService: (any CatalogServicing)?
    private let musicCatalog: (any MusicCatalogServicing)?
    private let musicPlayer: (any RoomMusicPlaying)?
    private let bundleRetention: (any AssetBundleRetaining)?

    private var loadGeneration = 0

    public init(
        room: Room,
        viewerRole: RoomViewerRole = .owner,
        design: any RoomDesignProviding,
        textures: RoomPhotoTextureProviding,
        accessToken: String,
        photoService: (any RoomPhotoServicing)? = nil,
        roomService: (any MuseumServicing)? = nil,
        photoReplacer: (any RoomPhotoReplacing)? = nil,
        sculptureModels: (any SculptureModelProviding)? = nil,
        catalogService: (any CatalogServicing)? = nil,
        musicCatalog: (any MusicCatalogServicing)? = nil,
        musicPlayer: (any RoomMusicPlaying)? = nil,
        bundleRetention: (any AssetBundleRetaining)? = nil,
        extendedWaitThreshold: Duration = RoomEntryViewModel.defaultExtendedWaitThreshold
    ) {
        self.room = room
        self.viewerRole = viewerRole
        self.design = design
        self.textures = textures
        self.accessToken = accessToken
        self.photoService = photoService
        self.roomService = roomService
        self.photoReplacer = photoReplacer
        self.sculptureModels = sculptureModels
        self.catalogService = catalogService
        self.musicCatalog = musicCatalog
        self.musicPlayer = musicPlayer
        self.bundleRetention = bundleRetention
        self.extendedWaitThreshold = extendedWaitThreshold
    }

    public func load() async {
        loadGeneration += 1
        let generation = loadGeneration
        state = .checking
        content = nil
        showsExtendedWaitCopy = false

        let threshold = extendedWaitThreshold
        let waitClock = Task { [weak self] in
            try? await Task.sleep(for: threshold)
            guard !Task.isCancelled else { return }
            await MainActor.run { [weak self] in
                guard let self, self.loadGeneration == generation, self.state == .checking else { return }
                self.showsExtendedWaitCopy = true
            }
        }
        defer { waitClock.cancel() }

        let resolution = await design.design(forVariantID: room.variantID) { [weak self] progress in
            Task { @MainActor [weak self] in
                self?.apply(progress, generation: generation)
            }
        }
        guard loadGeneration == generation, !Task.isCancelled else { return }

        switch resolution {
        case .unavailable(let reason):
            state = .designUnavailable(variantID: room.variantID, reason: reason)

        case .available(let design):
            switch RoomPlacementResolver.resolve(room: room, slotTable: design.slotTable) {
            case .unresolvable(let failure):
                state = .placementUnresolvable(failure)
            case .resolved(let placements):
                content = RoomRuntimeContent(
                    roomID: room.id,
                    accessToken: accessToken,
                    geometry: design.geometry,
                    viewerRole: viewerRole,
                    room: room,
                    slotTable: design.slotTable,
                    placements: placements,
                    textures: textures,
                    photoService: photoService,
                    roomService: roomService,
                    photoReplacer: photoReplacer,
                    sculptureModels: sculptureModels,
                    catalogService: catalogService,
                    musicCatalog: musicCatalog,
                    musicPlayer: musicPlayer,
                    bundleRetention: bundleRetention
                )
                state = .ready
            }
        }
    }

    private func apply(_ progress: RoomDesignLoadState, generation: Int) {
        guard generation == loadGeneration, !state.isTerminal else { return }
        switch progress {
        case .checking, .ready:
            return
        case .downloading(let fraction):
            switch state {
            case .geometryReady: return
            case .downloading(let current) where fraction < current: return
            default: state = .downloading(fractionComplete: fraction)
            }
        case .geometryReady(let fraction):
            state = .geometryReady(fractionComplete: max(fraction, state.fractionComplete))
        }
    }

    // MARK: - Copy

    public var contentSummary: String {
        let count = room.photoSlots.count
        guard count > 0 else { return "No photographs yet." }
        let layout = RoomPhotoSlotLayout.slots(forPhotoCount: count)
        var byWall: [RoomWall: Int] = [:]
        for slot in layout { byWall[slot.wall, default: 0] += 1 }
        let walls = RoomWall.allCases.compactMap { wall -> String? in
            guard let n = byWall[wall], n > 0 else { return nil }
            return "\(n) \(wall.rawValue)"
        }
        return "\(count) photograph\(count == 1 ? "" : "s") · " + walls.joined(separator: " · ")
    }

    public func designUnavailableCopy(_ reason: RoomDesignUnavailableReason) -> (title: String, message: String, canRetry: Bool) {
        switch reason {
        case .notPublished:
            return ("Design not available yet",
                    "This Room's design isn't available yet, so it can't be entered in 3D. "
                    + "Your photographs are saved and will hang in place as soon as the design arrives.",
                    true)
        case .deliveryUnconfigured:
            return ("Designs can't be delivered right now",
                    "This server isn't set up to deliver Room designs. Your photographs are safe.",
                    true)
        case .variantUnknown:
            return ("Design not recognised",
                    "This Room refers to a design that isn't in the catalog. Try again later.",
                    true)
        case .offline:
            return ("You're offline",
                    "This Room's design hasn't been downloaded yet, so it needs a connection. Rooms you've already visited can still be explored offline.",
                    true)
        case .network:
            return ("Couldn't download the design",
                    "Check your connection and try again. Anything already downloaded is kept, so a retry picks up where it left off.",
                    true)
        case .corruptDownload:
            return ("The download didn't arrive intact",
                    "The design's files didn't match what was expected, so they were discarded. Try again to download them afresh.",
                    true)
        case .storage:
            return ("Couldn't save the design",
                    "There wasn't room to store this design on the device. Free some space and try again.",
                    true)
        case .malformedBundle:
            return ("This design can't be used",
                    "The design's files aren't in a form this version of Muse can use. Updating the app may help.",
                    false)
        case .layoutMismatch:
            return ("This Room's design data doesn't match its design",
                    "Try again later.",
                    true)
        }
    }

    public func loadingCopy(for state: State) -> (title: String, detail: String?) {
        switch state {
        case .checking:
            return showsExtendedWaitCopy
                ? ("Still preparing your Room", "This can take a moment the first time a design is used on this device.")
                : ("Preparing your Room", nil)
        case .downloading(let fraction):
            return ("Downloading this Room's design", "\(Int(fraction * 100))% · Later visits will be near-instant.")
        case .geometryReady(let fraction):
            return ("Geometry ready — finishing up", "\(Int(fraction * 100))%")
        case .ready:
            return ("Entering…", nil)
        case .designUnavailable(_, let reason):
            let copy = designUnavailableCopy(reason)
            return (copy.title, copy.message)
        case .placementUnresolvable(let failure):
            return ("Can't lay out this Room", placementUnresolvableMessage(failure))
        }
    }

    public func placementUnresolvableMessage(_ failure: RoomPlacementFailure) -> String {
        switch failure {
        case .slotTableUnavailable:
            return designUnavailableCopy(.notPublished).message
        case .variantMismatch:
            return "This Room's design data doesn't match its design. Try again later."
        case .unsupportedPhotoCount(let count):
            return "This Room holds \(count) photographs, more than a Room supports."
        case .anchorMissingFromTable(let anchor):
            return "This Room's design is missing a mounting point (\(anchor.wall.rawValue) \(anchor.positionOnWall + 1))."
        }
    }
}
