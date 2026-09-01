import Foundation

public enum PreviewAssetAvailability: Equatable, Sendable {
    case unavailable
    case unreachable
    case downloading(fractionComplete: Double)
    case geometryReady
    case ready
}

public protocol PreviewAssetProviding: Sendable {
    func availability(for subject: PreviewSubject) async -> PreviewAssetAvailability
}

public struct UnavailablePreviewAssetProvider: PreviewAssetProviding {
    public init() {}

    public func availability(for subject: PreviewSubject) async -> PreviewAssetAvailability {
        .unavailable
    }
}
