import Foundation

@MainActor
public final class CollectionRoomListViewModel {

    public enum State: Equatable {
        case loading
        case empty
        case loaded(rooms: [CollectionRoom])
        case failed(message: String)
    }

    public private(set) var state: State = .loading {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    private let collections: any CollectionServicing
    private let catalog: any CollectionCatalogServicing
    private let accessToken: String

    private var categoryNames: [String: String] = [:]

    private let refresh = RefreshCoordination()

    public private(set) var refreshFailureNotice: String?

    public init(
        collections: any CollectionServicing,
        catalog: any CollectionCatalogServicing,
        accessToken: String
    ) {
        self.collections = collections
        self.catalog = catalog
        self.accessToken = accessToken
    }

    public func load() async {
        let token = refresh.begin()
        if !hasRooms { state = .loading }
        do {
            if let categories = try? await catalog.fetchCollectionCategories(accessToken: accessToken) {
                categoryNames = Dictionary(uniqueKeysWithValues: categories.map { ($0.id, $0.displayName) })
            }

            let rooms = try await collections.listCollectionRooms(accessToken: accessToken)
            guard refresh.isCurrent(token) else { return }
            refreshFailureNotice = nil
            state = rooms.isEmpty ? .empty : .loaded(rooms: rooms)
        } catch {
            guard refresh.isCurrent(token) else { return }
            if hasRooms {
                refreshFailureNotice = RefreshFailureNotice.message(for: error)
                onStateChange?(state)
            } else {
                refreshFailureNotice = nil
                state = .failed(message: NetworkFailureCopy.message(for: error, operation: .read, otherwise: "Couldn't load your Collection Rooms. Please try again."))
            }
        }
    }

    private var hasRooms: Bool {
        if case .loaded(let rooms) = state { return !rooms.isEmpty }
        return false
    }

    public func insert(_ room: CollectionRoom) {
        switch state {
        case .loaded(let rooms):
            state = .loaded(rooms: rooms + [room])
        case .empty, .loading, .failed:
            state = .loaded(rooms: [room])
        }
    }

    public func categoryName(for room: CollectionRoom) -> String? {
        guard let categoryID = room.categoryID else { return nil }
        return categoryNames[categoryID]
    }

    public var rooms: [CollectionRoom] {
        if case .loaded(let rooms) = state { return rooms }
        return []
    }

    // MARK: - Copy

    public static let emptyMessage =
        "You don't have any Collection Rooms yet. Create one for each collection you want to display."

    public static let countCap: Int? = nil
}
