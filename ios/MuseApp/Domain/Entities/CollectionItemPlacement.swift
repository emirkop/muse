import Foundation

public enum CollectionItemPlacement {

    public enum Drop: Equatable, Sendable {
        case noChange
        case swap(with: CollectionItem)
        case move(toSlot: Int)
        case rejected(Rejection)
    }

    public enum Rejection: Equatable, Sendable {
        case tierNotReached
        case slotNotAuthored
        case itemNotInRoom
    }

    public static func resolveDrop(
        items: [CollectionItem],
        movingItemID: String,
        toSlot targetSlotIndex: Int,
        availableSlotIndices: Set<Int>,
        authoredSlotIndices: Set<Int>
    ) -> Drop {
        guard let moving = items.first(where: { $0.id == movingItemID }) else {
            return .rejected(.itemNotInRoom)
        }
        if moving.slotIndex == targetSlotIndex {
            return .noChange
        }
        guard authoredSlotIndices.contains(targetSlotIndex) else {
            return .rejected(.slotNotAuthored)
        }
        guard availableSlotIndices.contains(targetSlotIndex) else {
            return .rejected(.tierNotReached)
        }
        if let occupant = items.first(where: { $0.slotIndex == targetSlotIndex }) {
            return .swap(with: occupant)
        }
        return .move(toSlot: targetSlotIndex)
    }

    public static func applying(_ drop: Drop, to items: [CollectionItem], movingItemID: String) -> [CollectionItem] {
        switch drop {
        case .noChange, .rejected:
            return items
        case .move(let target):
            return items.map { item in
                item.id == movingItemID
                    ? CollectionItem(id: item.id, slotIndex: target, catalogModelID: item.catalogModelID)
                    : item
            }
            .sorted { $0.slotIndex < $1.slotIndex }
        case .swap(let occupant):
            guard let moving = items.first(where: { $0.id == movingItemID }) else { return items }
            return items.map { item in
                switch item.id {
                case movingItemID:
                    return CollectionItem(
                        id: item.id, slotIndex: occupant.slotIndex, catalogModelID: item.catalogModelID
                    )
                case occupant.id:
                    return CollectionItem(
                        id: item.id, slotIndex: moving.slotIndex, catalogModelID: item.catalogModelID
                    )
                default:
                    return item
                }
            }
            .sorted { $0.slotIndex < $1.slotIndex }
        }
    }

    public static func lowestFreeSlot(
        items: [CollectionItem],
        availableSlotIndices: Set<Int>
    ) -> Int? {
        let occupied = Set(items.map(\.slotIndex))
        return availableSlotIndices.subtracting(occupied).min()
    }
}

public extension CollectionRoom {
    func replacingItems(_ items: [CollectionItem]) -> CollectionRoom {
        CollectionRoom(
            id: id,
            name: name,
            categoryID: categoryID,
            designID: designID,
            currentTier: currentTier,
            items: items
        )
    }
}

public extension CollectionTierTable {
    var authoredSlotIndices: Set<Int> {
        Set(tiers.flatMap { $0.itemTransforms.map(\.slotIndex) })
    }

    func availableSlotIndices(atTier tier: CollectionTier) -> Set<Int> {
        Set(availableSlots(atTier: tier).map(\.slotIndex))
    }

    func resolvePlacements(
        for items: [CollectionItem],
        atTier tier: CollectionTier
    ) -> (placed: [(item: CollectionItem, slot: CollectionItemSlot)], unresolvable: [CollectionItem]) {
        var slotsByIndex: [Int: CollectionItemSlot] = [:]
        for slot in availableSlots(atTier: tier) {
            slotsByIndex[slot.slotIndex] = slot
        }

        var placed: [(item: CollectionItem, slot: CollectionItemSlot)] = []
        var unresolvable: [CollectionItem] = []
        for item in items.sorted(by: { $0.slotIndex < $1.slotIndex }) {
            if let slot = slotsByIndex[item.slotIndex] {
                placed.append((item, slot))
            } else {
                unresolvable.append(item)
            }
        }
        return (placed, unresolvable)
    }
}
