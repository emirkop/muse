import Foundation
import simd

public struct CollectionItemSlot: Equatable, Sendable {
    public let slotIndex: Int
    public let transform: SlotTransform

    public init(slotIndex: Int, transform: SlotTransform) {
        self.slotIndex = slotIndex
        self.transform = transform
    }
}

public struct CollectionTierTable: Equatable, Sendable {
    public struct Tier: Equatable, Sendable {
        public let ordinal: Int
        public let cumulativeCapacity: Int
        public let itemTransforms: [CollectionItemSlot]
        public let additionalGeometry: AssetBundleRef?

        public init(
            ordinal: Int,
            cumulativeCapacity: Int,
            itemTransforms: [CollectionItemSlot],
            additionalGeometry: AssetBundleRef? = nil
        ) {
            self.ordinal = ordinal
            self.cumulativeCapacity = cumulativeCapacity
            self.itemTransforms = itemTransforms
            self.additionalGeometry = additionalGeometry
        }
    }

    public let designID: String
    public let tiers: [Tier]
    public let entry: SlotTransform

    public init(designID: String, tiers: [Tier], entry: SlotTransform) {
        self.designID = designID
        self.tiers = tiers
        self.entry = entry
    }

    public enum Rejection: Equatable, Sendable {
        case noTiers
        case tiersNotSequentialFromOne
        case capacitiesNotStrictlyIncreasing
        case slotCountDoesNotMatchAddedCapacity(tier: Int)
        case slotIndicesNotContiguous
    }

    public var rejection: Rejection? {
        if tiers.isEmpty { return .noTiers }

        var previousCapacity = 0
        var expectedSlotIndex = 0
        for (offset, tier) in tiers.enumerated() {
            if tier.ordinal != CollectionTier.base.ordinal + offset {
                return .tiersNotSequentialFromOne
            }
            if tier.cumulativeCapacity <= previousCapacity {
                return .capacitiesNotStrictlyIncreasing
            }
            let added = tier.cumulativeCapacity - previousCapacity
            if tier.itemTransforms.count != added {
                return .slotCountDoesNotMatchAddedCapacity(tier: tier.ordinal)
            }
            for slot in tier.itemTransforms {
                if slot.slotIndex != expectedSlotIndex {
                    return .slotIndicesNotContiguous
                }
                expectedSlotIndex += 1
            }
            previousCapacity = tier.cumulativeCapacity
        }
        return nil
    }

    public var capacities: CollectionTierCapacities {
        CollectionTierCapacities(tiers.map(\.cumulativeCapacity))
    }

    public var highestTier: CollectionTier? {
        guard let last = tiers.last else { return nil }
        return CollectionTier(last.ordinal)
    }

    public func availableSlots(atTier tier: CollectionTier) -> [CollectionItemSlot] {
        var slots: [CollectionItemSlot] = []
        for authored in tiers where authored.ordinal <= tier.ordinal {
            slots.append(contentsOf: authored.itemTransforms)
        }
        return slots
    }

    public func capacity(atTier tier: CollectionTier) -> Int? {
        tiers.first(where: { $0.ordinal == tier.ordinal })?.cumulativeCapacity
    }

    public func slot(forSlotIndex slotIndex: Int, atTier tier: CollectionTier) -> CollectionItemSlot? {
        guard slotIndex >= 0 else { return nil }
        var consumed = 0
        for authored in tiers {
            let next = consumed + authored.itemTransforms.count
            if slotIndex < next {
                guard authored.ordinal <= tier.ordinal else { return nil }
                return authored.itemTransforms[slotIndex - consumed]
            }
            consumed = next
        }
        return nil
    }

    public func additionalGeometry(upToTier tier: CollectionTier) -> [AssetBundleRef] {
        tiers
            .filter { $0.ordinal <= tier.ordinal }
            .compactMap(\.additionalGeometry)
    }

    public func additionalGeometry(
        movingFrom from: CollectionTier,
        to: CollectionTier
    ) -> [AssetBundleRef] {
        tiers
            .filter { $0.ordinal > from.ordinal && $0.ordinal <= to.ordinal }
            .compactMap(\.additionalGeometry)
    }
}
