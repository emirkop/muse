import Foundation

public struct CollectionTier: Equatable, Comparable, Sendable {
    public let ordinal: Int

    public init(_ ordinal: Int) { self.ordinal = ordinal }

    public static let base = CollectionTier(1)

    public static func < (lhs: CollectionTier, rhs: CollectionTier) -> Bool {
        lhs.ordinal < rhs.ordinal
    }
}

public struct CollectionTierCapacities: Equatable, Sendable {
    public let cumulative: [Int]

    public init(_ cumulative: [Int]) { self.cumulative = cumulative }

    public enum Rejection: Equatable, Sendable {
        case empty
        case notStrictlyIncreasing
    }

    public var rejection: Rejection? {
        if cumulative.isEmpty { return .empty }
        var previous = 0
        for capacity in cumulative {
            if capacity <= previous { return .notStrictlyIncreasing }
            previous = capacity
        }
        return nil
    }

    public var highestTier: CollectionTier? {
        guard rejection == nil else { return nil }
        return CollectionTier(CollectionTier.base.ordinal + cumulative.count - 1)
    }

    public func capacity(at tier: CollectionTier) -> Int? {
        guard rejection == nil, let highest = highestTier,
              tier >= CollectionTier.base, tier <= highest
        else { return nil }
        return cumulative[tier.ordinal - CollectionTier.base.ordinal]
    }

    public func requiredTier(forItemCount itemCount: Int) -> Result<CollectionTier, TierResolutionFailure> {
        if let rejection {
            return .failure(.malformedTable(rejection))
        }
        guard itemCount >= 0 else { return .failure(.negativeItemCount) }
        for (index, capacity) in cumulative.enumerated() where itemCount <= capacity {
            return .success(CollectionTier(CollectionTier.base.ordinal + index))
        }
        return .failure(.exhausted)
    }
}

public enum TierResolutionFailure: Equatable, Error, Sendable {
    case malformedTable(CollectionTierCapacities.Rejection)
    case negativeItemCount
    case exhausted
}
