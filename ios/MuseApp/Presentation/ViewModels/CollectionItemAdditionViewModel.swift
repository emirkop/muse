import Foundation

@MainActor
public final class CollectionItemAdditionViewModel {

    public enum State: Equatable {
        case confirming
        case adding
        case added(slotIndex: Int, spaceGrew: Bool, newGeometryIncomplete: Bool)
        case designUnavailable
        case capacityReached
        case failed(message: String, retryable: Bool)
    }

    public private(set) var state: State = .confirming {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public private(set) var room: CollectionRoom

    public let model: CollectionCatalogModel

    private let addition: CollectionItemAddition
    private let tables: any CollectionDesignTableProviding
    private let accessToken: String

    public init(
        model: CollectionCatalogModel,
        room: CollectionRoom,
        addition: CollectionItemAddition,
        tables: any CollectionDesignTableProviding,
        accessToken: String
    ) {
        self.model = model
        self.room = room
        self.addition = addition
        self.tables = tables
        self.accessToken = accessToken
    }

    public var modelDescription: String {
        "\(model.brandDisplayName) \(model.displayName)"
    }

    public var assetNote: String {
        model.hasAsset
            ? "This model has a 3D asset. How it is displayed arrives with the Collection Room interior."
            : "This model has no 3D asset yet, so it will show as a placeholder."
    }

    public func confirm() async {
        guard let categoryID = room.categoryID, let designID = room.designID else {
            state = .designUnavailable
            return
        }

        state = .adding

        guard let table = await tables.tierTable(
            accessToken: accessToken, categoryID: categoryID, designID: designID
        ) else {
            state = .designUnavailable
            return
        }

        switch await addition.add(
            catalogModelID: model.id, to: room, table: table, accessToken: accessToken
        ) {
        case .success(let outcome):
            room = outcome.room
            state = .added(
                slotIndex: outcome.placedItem.slotIndex,
                spaceGrew: outcome.expansion?.expanded == true,
                newGeometryIncomplete: outcome.newGeometryIncomplete
            )
        case .failure(.capacityReached):
            state = .capacityReached
        case .failure(let failure):
            state = .failed(message: Self.message(for: failure), retryable: Self.isRetryable(failure))
        }
    }

    private static func message(for failure: CollectionItemAddition.Failure) -> String {
        switch failure {
        case .modelNotAvailable:
            return "That model can't be added to this collection."
        case .roomUnavailable:
            return "This collection room is no longer available."
        case .expansionFailed(.capacityExhausted):
            return "This room's design is full. It can't grow any further."
        case .expansionFailed(.incoherentTable), .expansionFailed(.negativeItemCount):
            return "This room's design couldn't be read. Try again later."
        case .expansionFailed(.ratchetRefused):
            return "Couldn't grow the room to fit another item. Check your connection and try again."
        case .placementFailed:
            return "Couldn't add the item. Check your connection and try again."
        case .capacityReached:
            return "You've reached your collection capacity."
        }
    }

    private static func isRetryable(_ failure: CollectionItemAddition.Failure) -> Bool {
        switch failure {
        case .modelNotAvailable, .expansionFailed(.capacityExhausted), .capacityReached:
            return false
        default:
            return true
        }
    }
}
