import Foundation

@MainActor
public final class RoomVariantSelectionViewModel {
    public enum Context: Equatable {
        case creatingRoom(name: String)
        case changingVariant(room: Room)
    }

    public enum State: Equatable {
        case loading
        case ready(variants: [RoomVariant])
        case applying
        case applied(Room)
        case failed(message: String, variants: [RoomVariant])
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public let context: Context

    private let museumService: MuseumServicing
    private let catalogService: CatalogServicing
    private let accessToken: String
    private let analytics: any AnalyticsRecording
    private let styleID: String
    private var loadedVariants: [RoomVariant] = []

    public init(
        context: Context,
        museumService: MuseumServicing,
        catalogService: CatalogServicing,
        accessToken: String,
        styleID: String,
        analytics: any AnalyticsRecording = NoAnalytics()
    ) {
        self.analytics = analytics
        self.context = context
        self.museumService = museumService
        self.catalogService = catalogService
        self.accessToken = accessToken
        self.styleID = styleID
    }

    public func load() async {
        state = .loading
        do {
            loadedVariants = try await catalogService.fetchVariants(accessToken: accessToken, styleID: styleID)
            state = .ready(variants: loadedVariants)
            analytics.record(.roomCreationStep(.variantListShown))
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load room designs. Please try again."), variants: [])
        }
    }

    public var currentVariantID: String? {
        if case .changingVariant(let room) = context { return room.variantID }
        return nil
    }

    public func isCurrentlySelected(_ variant: RoomVariant) -> Bool {
        variant.id == currentVariantID
    }

    public var confirmationReassurance: String? {
        if case .changingVariant = context {
            return "Your photos stay exactly where they are."
        }
        return nil
    }

    public func chooseVariant(_ variantID: String) async {
        state = .applying
        do {
            let room: Room
            switch context {
            case .creatingRoom(let name):
                room = try await museumService.createRoom(
                    accessToken: accessToken,
                    name: name,
                    variantID: variantID
                )
            case .changingVariant(let existing):
                room = try await museumService.updateRoom(
                    accessToken: accessToken,
                    roomID: existing.id,
                    patch: .variant(variantID)
                )
            }
            state = .applied(room)
            analytics.record(.roomCreationStep(.variantConfirmed))
        } catch {
            state = .failed(
                message: NetworkFailureCopy.mutationOutcome(
                    for: error,
                    certainlyUnchanged: NetworkFailureCopy.message(
                        for: error, operation: .mutation,
                        otherwise: "Couldn't apply that design. Please try again."),
                    possiblyApplied: "Couldn't confirm that design. \(NetworkFailureCopy.outcomeUnknownTail)"
                ),
                variants: loadedVariants
            )
        }
    }
}
