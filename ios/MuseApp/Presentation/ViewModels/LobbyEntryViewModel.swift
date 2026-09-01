import Foundation

@MainActor
public final class LobbyEntryViewModel {
    public enum State: Equatable {
        case checking
        case noVisibleRooms
        case enterRoomDirectly(Room)
        case lobbyDesignUnavailable(styleID: String)
        case placementUnresolvable(LobbyCardPlacementFailure)
        case noLongerAvailable
        case ready
        case failed(message: String)
    }

    public private(set) var state: State = .checking {
        didSet {
            guard state != oldValue else { return }
            onStateChange?(state)
        }
    }
    public var onStateChange: ((State) -> Void)?

    public private(set) var content: LobbyRuntimeContent?
    public private(set) var visibleCards: [LobbyRoomCard] = []

    private let viewerRole: MuseumViewerRole
    private let contentSource: any MuseumLobbyContentProviding
    private let geometry: any LobbyGeometryProviding
    private let cardTables: any LobbyCardTableProviding
    private let accessToken: String

    public init(
        viewerRole: MuseumViewerRole,
        contentSource: any MuseumLobbyContentProviding,
        geometry: any LobbyGeometryProviding,
        cardTables: any LobbyCardTableProviding,
        accessToken: String
    ) {
        self.viewerRole = viewerRole
        self.contentSource = contentSource
        self.geometry = geometry
        self.cardTables = cardTables
        self.accessToken = accessToken
    }

    public func load() async {
        state = .checking
        content = nil

        let content: MuseumLobbyContent
        do {
            content = try await contentSource.lobbyContent(accessToken: accessToken)
        } catch IdentityAPIClientError.server(let statusCode, _) where statusCode == 404 && viewerRole == .visitor {
            state = .noLongerAvailable
            return
        } catch {
            state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: failureMessage))
            return
        }
        let museum = Museum(id: content.museumID, styleID: content.styleID, privacy: .public)
        let rooms = content.rooms

        let cards = MuseumLobbyVisibility.visibleCards(rooms: rooms, viewerRole: viewerRole)
        visibleCards = cards

        switch MuseumLobbyRouting.route(forVisible: cards) {
        case .noVisibleRooms:
            state = .noVisibleRooms

        case .enterRoomDirectly(let roomID):
            guard let room = rooms.first(where: { $0.id == roomID }) else {
                state = .failed(message: failureMessage)
                return
            }
            state = .enterRoomDirectly(room)

        case .lobby(let cards):
            await prepareLobby(museum: museum, cards: cards)
        }
    }

    private func prepareLobby(museum: Museum, cards: [LobbyRoomCard]) async {
        guard let geometry = await geometry.lobbyGeometry(forStyleID: museum.styleID),
              let table = await cardTables.cardTable(forStyleID: museum.styleID, cardCount: cards.count) else {
            state = .lobbyDesignUnavailable(styleID: museum.styleID)
            return
        }

        switch LobbyCardPlacementResolver.resolve(cards: cards, styleID: museum.styleID, table: table) {
        case .unresolvable(let failure):
            state = .placementUnresolvable(failure)
        case .resolved(let placements):
            content = LobbyRuntimeContent(
                museumID: museum.id,
                styleID: museum.styleID,
                geometry: geometry,
                viewerRole: viewerRole,
                cardTable: table,
                placements: placements
            )
            state = .ready
        }
    }

    // MARK: - Copy

    public var contentSummary: String {
        let count = visibleCards.count
        guard count > 0 else { return "" }
        let privateCount = visibleCards.filter(\.isMarkedPrivate).count
        var summary = "\(count) Room\(count == 1 ? "" : "s")"
        if privateCount > 0 {
            summary += " · \(privateCount) Private"
        }
        return summary
    }

    public var isVisitor: Bool { viewerRole == .visitor }

    private var failureMessage: String {
        switch viewerRole {
        case .owner: return "Couldn't open your Museum. Please try again."
        case .visitor: return "Couldn't open this Museum. Please try again."
        }
    }

    public var noLongerAvailableMessage: String {
        "This link is no longer available."
    }

    public var noVisibleRoomsMessage: String {
        switch viewerRole {
        case .owner:
            return "Your Museum has no Rooms yet. Create one and it will be waiting in your Lobby."
        case .visitor:
            return "No public Rooms yet."
        }
    }

    public var designUnavailableMessage: String {
        switch viewerRole {
        case .owner:
            return "Your Museum's Lobby design isn't available yet, so it can't be entered in 3D. "
                + "Your Rooms are saved and will appear here as soon as the design arrives."
        case .visitor:
            return "This Museum's Lobby design isn't available yet, so it can't be entered in 3D."
        }
    }

    public func placementUnresolvableMessage(_ failure: LobbyCardPlacementFailure) -> String {
        switch failure {
        case .cardTableUnavailable:
            return designUnavailableMessage
        case .styleMismatch:
            return "Your Museum's Lobby design doesn't match its Style. Try again later."
        case .insufficientCardSpots(let needed, let available):
            return "This Lobby design has room for \(available) Room card\(available == 1 ? "" : "s"), "
                + "but your Museum has \(needed)."
        }
    }
}
