import Foundation

@MainActor
public final class StyleSelectionViewModel {
    public enum Context: Equatable {
        case creatingMuseum
        case changingStyle(currentStyleID: String)
    }

    public enum State: Equatable {
        case loading
        case ready(styles: [MuseumStyle])
        case applying
        case applied(Museum)
        case failed(message: String, styles: [MuseumStyle])
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
    private var loadedStyles: [MuseumStyle] = []

    public init(
        context: Context,
        museumService: MuseumServicing,
        catalogService: CatalogServicing,
        accessToken: String,
        analytics: any AnalyticsRecording = NoAnalytics()
    ) {
        self.analytics = analytics
        self.context = context
        self.museumService = museumService
        self.catalogService = catalogService
        self.accessToken = accessToken
    }

    public func load() async {
        state = .loading
        do {
            loadedStyles = try await catalogService.fetchStyles(accessToken: accessToken)
            state = .ready(styles: loadedStyles)
            analytics.record(.museumCreationStep(.styleListShown))
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load Museum styles. Please try again."), styles: [])
            analytics.record(.failureShown(
                surface: .styleSelection, classification: .of(error), retried: false, retrySucceeded: false))
        }
    }

    public var currentStyleID: String? {
        if case .changingStyle(let styleID) = context { return styleID }
        return nil
    }

    public func isCurrentlySelected(_ style: MuseumStyle) -> Bool {
        style.id == currentStyleID
    }

    public var requiresContentPreservationReassurance: Bool {
        if case .changingStyle = context { return true }
        return false
    }

    public var confirmationReassurance: String? {
        requiresContentPreservationReassurance
            ? "Your Rooms, photos, and content stay exactly as they are."
            : nil
    }

    public func chooseStyle(_ styleID: String) async {
        state = .applying
        do {
            let museum: Museum
            switch context {
            case .creatingMuseum:
                museum = try await museumService.createMuseum(accessToken: accessToken, styleID: styleID)
            case .changingStyle:
                museum = try await museumService.changeStyle(accessToken: accessToken, styleID: styleID)
            }
            state = .applied(museum)
            analytics.record(.museumCreationStep(.styleConfirmed))
        } catch IdentityAPIClientError.server(let statusCode, _) where statusCode == 409 {
            state = .failed(message: "You already have a Museum.", styles: loadedStyles)
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .mutation, otherwise: "Couldn't apply that style. Please try again."), styles: loadedStyles)
        }
    }

    public static let openStyleGateNotice =
        "Two further Museum Styles are planned but not yet defined. They'll appear here once confirmed."
}
