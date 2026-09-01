import Foundation

public struct RecognitionConfidence: Equatable, Comparable, Sendable {
    public let value: Double

    public init(_ value: Double) {
        self.value = min(max(value, 0), 1)
    }

    public static func < (lhs: RecognitionConfidence, rhs: RecognitionConfidence) -> Bool {
        lhs.value < rhs.value
    }
}

public struct RecognitionCandidate: Equatable, Sendable {
    public let catalogModelID: String
    public let confidence: RecognitionConfidence

    public init(catalogModelID: String, confidence: RecognitionConfidence) {
        self.catalogModelID = catalogModelID
        self.confidence = confidence
    }
}

public enum RecognitionOutcome: Equatable, Sendable {
    case candidates(Candidates)

    case noMatch(suggestedQuery: String?)

    case unavailable(reason: UnavailableReason)

    public enum UnavailableReason: Equatable, Sendable {
        case noProviderConfigured
        case inputUnavailable
        case attemptFailed
    }

    public var routesToManualSearchFallback: Bool {
        switch self {
        case .candidates: return false
        case .noMatch, .unavailable: return true
        }
    }

    public var suggestedManualSearchQuery: String? {
        if case .noMatch(let query) = self { return query }
        return nil
    }
}

public struct Candidates: Equatable, Sendable {
    public let ordered: [RecognitionCandidate]

    public init?(_ candidates: [RecognitionCandidate]) {
        guard !candidates.isEmpty else { return nil }
        self.ordered = candidates.enumerated()
            .sorted { left, right in
                if left.element.confidence != right.element.confidence {
                    return left.element.confidence > right.element.confidence
                }
                return left.offset < right.offset
            }
            .map(\.element)
    }

    public var top: RecognitionCandidate { ordered[0] }

    public var count: Int { ordered.count }
}
