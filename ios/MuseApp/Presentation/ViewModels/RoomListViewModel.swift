import Foundation

@MainActor
public final class RoomListViewModel {
    public enum State: Equatable {
        case loading
        case loaded(rooms: [Room])
        case failed(message: String)
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let museumService: MuseumServicing
    private let accessToken: String
    private let refresh = RefreshCoordination()

    public private(set) var refreshFailureNotice: String?

    public init(museumService: MuseumServicing, accessToken: String) {
        self.museumService = museumService
        self.accessToken = accessToken
    }

    public func load() async {
        let token = refresh.begin()
        if !hasRooms { state = .loading }
        do {
            let rooms = try await museumService.listRooms(accessToken: accessToken)
            guard refresh.isCurrent(token) else { return }
            refreshFailureNotice = nil
            state = .loaded(rooms: rooms)
        } catch {
            guard refresh.isCurrent(token) else { return }
            if hasRooms {
                refreshFailureNotice = RefreshFailureNotice.message(for: error)
                notifyStateUnchanged()
            } else {
                refreshFailureNotice = nil
                state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load your Rooms. Please try again."))
            }
        }
    }

    private var hasRooms: Bool {
        if case .loaded(let rooms) = state { return !rooms.isEmpty }
        return false
    }

    private func notifyStateUnchanged() {
        onStateChange?(state)
    }

    public var canCreateRoom: Bool {
        guard let cap = RoomCreationViewModel.roomCountCap else { return true }
        if case .loaded(let rooms) = state { return rooms.count < cap }
        return true
    }
}
