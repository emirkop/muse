import Foundation

public struct CollectionItemAddition: Sendable {

    public struct Outcome: Equatable, Sendable {
        public let room: CollectionRoom
        public let placedItem: CollectionItem
        public let expansion: CollectionTierExpansion.Outcome?

        public var newGeometryIncomplete: Bool {
            guard let expansion else { return false }
            return !expansion.isFullyRendered
        }
    }

    public enum Failure: Error, Equatable {
        case expansionFailed(CollectionTierExpansion.Failure)
        case modelNotAvailable
        case roomUnavailable
        case capacityReached
        case placementFailed
    }

    private let expansion: CollectionTierExpansion
    private let items: any CollectionItemPlacing

    public init(expansion: CollectionTierExpansion, items: any CollectionItemPlacing) {
        self.expansion = expansion
        self.items = items
    }

    public func add(
        catalogModelID: String,
        to room: CollectionRoom,
        table: CollectionTierTable,
        accessToken: String
    ) async -> Result<Outcome, Failure> {
        let targetCount = room.itemCount + 1

        var expansionOutcome: CollectionTierExpansion.Outcome?
        switch CollectionTierExpansion.requiredTier(forItemCount: targetCount, table: table) {
        case .failure(let failure):
            return .failure(.expansionFailed(failure))
        case .success(let required):
            if required > room.currentTier {
                switch await expansion.expand(
                    room: room, toHold: targetCount, table: table, accessToken: accessToken
                ) {
                case .failure(let failure):
                    return .failure(.expansionFailed(failure))
                case .success(let outcome):
                    expansionOutcome = outcome
                }
            }
        }

        let updated: CollectionRoom
        do {
            updated = try await items.addItem(
                accessToken: accessToken,
                collectionRoomID: room.id,
                catalogModelID: catalogModelID
            )
        } catch let error as CollectionAPIError {
            if error.isItemCapacityReached { return .failure(.capacityReached) }
            if error.isModelNotAvailable { return .failure(.modelNotAvailable) }
            if error.statusCode == 404 { return .failure(.roomUnavailable) }
            return .failure(.placementFailed)
        } catch {
            return .failure(.placementFailed)
        }

        let existing = Set(room.items.map(\.id))
        guard let placed = updated.items.first(where: { !existing.contains($0.id) }) else {
            return .failure(.placementFailed)
        }

        return .success(Outcome(room: updated, placedItem: placed, expansion: expansionOutcome))
    }
}
