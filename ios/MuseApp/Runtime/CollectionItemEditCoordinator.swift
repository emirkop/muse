import Foundation

@MainActor
public final class CollectionItemEditCoordinator {

    public enum Status: Equatable {
        case idle
        case saving
        case saved
        case rejected(CollectionItemPlacement.Rejection)
        case busy
        case failedTransport
        case failedStale
        case failedInvalid
    }

    public private(set) var room: CollectionRoom
    public private(set) var placements: [(item: CollectionItem, slot: CollectionItemSlot)] = []
    public private(set) var unresolvableItems: [CollectionItem] = []
    public private(set) var status: Status = .idle

    public var onPlacementsChanged: (([(item: CollectionItem, slot: CollectionItemSlot)]) -> Void)?
    public var onStatusChanged: ((Status) -> Void)?

    private let table: CollectionTierTable
    private let items: any CollectionItemPlacing
    private let rooms: any CollectionServicing
    private let accessToken: String

    private var serverItems: [CollectionItem]
    private var isSending = false
    private var isActive = true

    public init(
        room: CollectionRoom,
        table: CollectionTierTable,
        items: any CollectionItemPlacing,
        rooms: any CollectionServicing,
        accessToken: String
    ) {
        self.room = room
        self.table = table
        self.items = items
        self.rooms = rooms
        self.accessToken = accessToken
        self.serverItems = room.items
        resolvePlacements()
    }

    public var availableSlotIndices: Set<Int> {
        table.availableSlotIndices(atTier: room.currentTier)
    }

    public var authoredSlotIndices: Set<Int> {
        table.authoredSlotIndices
    }

    public func drop(itemID: String, onSlot slotIndex: Int) {
        guard isActive else { return }

        let drop = CollectionItemPlacement.resolveDrop(
            items: room.items,
            movingItemID: itemID,
            toSlot: slotIndex,
            availableSlotIndices: availableSlotIndices,
            authoredSlotIndices: authoredSlotIndices
        )

        switch drop {
        case .noChange:
            report(.idle)
            return
        case .rejected(let rejection):
            report(.rejected(rejection))
            return
        case .move, .swap:
            break
        }

        guard !isSending else {
            report(.busy)
            return
        }

        room = room.replacingItems(
            CollectionItemPlacement.applying(drop, to: room.items, movingItemID: itemID)
        )
        resolvePlacements()
        isSending = true
        report(.saving)

        Task { [weak self] in
            await self?.persist(itemID: itemID, slotIndex: slotIndex)
        }
    }

    @discardableResult
    public func adopt(_ updated: CollectionRoom) -> Bool {
        guard isActive, !isSending, updated.id == room.id else { return false }
        room = updated
        serverItems = updated.items
        resolvePlacements()
        report(.idle)
        return true
    }

    public func deactivate() {
        isActive = false
    }

    // MARK: - Persistence

    private func persist(itemID: String, slotIndex: Int) async {
        do {
            let updated = try await items.placeItem(
                accessToken: accessToken,
                collectionRoomID: room.id,
                collectionItemID: itemID,
                slotIndex: slotIndex
            )
            guard isActive else { return }
            room = updated
            serverItems = updated.items
            resolvePlacements()
            isSending = false
            report(.saved)
        } catch let error as CollectionAPIError {
            guard isActive else { return }
            if error.isSlotTaken || error.isItemNotInRoom || error.statusCode == 404 {
                await rollback(then: .failedStale, reloading: true)
            } else if (400..<500).contains(error.statusCode) {
                await rollback(then: .failedInvalid, reloading: false)
            } else {
                await rollback(then: .failedTransport, reloading: false)
            }
        } catch {
            guard isActive else { return }
            await rollback(then: .failedTransport, reloading: false)
        }
    }

    private func rollback(then status: Status, reloading: Bool) async {
        room = room.replacingItems(serverItems)
        resolvePlacements()
        isSending = false
        report(status)

        guard reloading else { return }
        if let reloaded = try? await rooms.fetchCollectionRoom(
            accessToken: accessToken, collectionRoomID: room.id
        ) {
            guard isActive, !isSending else { return }
            room = reloaded
            serverItems = reloaded.items
            resolvePlacements()
        }
    }

    // MARK: - Placement resolution

    private func resolvePlacements() {
        let resolved = table.resolvePlacements(for: room.items, atTier: room.currentTier)
        placements = resolved.placed
        unresolvableItems = resolved.unresolvable
        onPlacementsChanged?(placements)
    }

    private func report(_ newStatus: Status) {
        status = newStatus
        onStatusChanged?(newStatus)
    }
}
