import Foundation

@MainActor
public final class PreviewViewModel {
    public enum State: Equatable {
        case checkingAssets
        case assetsUnavailable
        case assetsUnreachable
        case downloading(fractionComplete: Double)
        case geometryReady
        case ready
    }

    public private(set) var state: State = .checkingAssets {
        didSet { onStateChange?(state) }
    }
    public var onStateChange: ((State) -> Void)?

    public let subject: PreviewSubject
    public let isCurrentlySelected: Bool
    public let confirmationReassurance: String?

    private let assetProvider: PreviewAssetProviding

    public init(
        subject: PreviewSubject,
        isCurrentlySelected: Bool,
        confirmationReassurance: String?,
        assetProvider: PreviewAssetProviding
    ) {
        self.subject = subject
        self.isCurrentlySelected = isCurrentlySelected
        self.confirmationReassurance = confirmationReassurance
        self.assetProvider = assetProvider
    }

    public func load() async {
        state = .checkingAssets
        switch await assetProvider.availability(for: subject) {
        case .unavailable:
            state = .assetsUnavailable
        case .unreachable:
            state = .assetsUnreachable
        case .downloading(let fraction):
            state = .downloading(fractionComplete: fraction)
        case .geometryReady:
            state = .geometryReady
        case .ready:
            state = .ready
        }
    }

    public var shouldPresentImmersiveSurface: Bool {
        switch state {
        case .geometryReady, .ready: return true
        case .checkingAssets, .assetsUnavailable, .assetsUnreachable, .downloading: return false
        }
    }

    public var statusMessage: String? {
        switch state {
        case .checkingAssets:
            return nil
        case .assetsUnavailable:
            return "This design's 3D environment hasn't been built yet, so there's nothing to preview. You can still choose it — your Museum will use it once the environment ships."
        case .assetsUnreachable:
            return "Couldn't load the preview — Muse can't be reached right now. You can still choose this design, or try again once you're back online."
        case .downloading(let fraction):
            return "Loading preview… \(Int(fraction * 100))%"
        case .geometryReady:
            return "Loading materials and lighting…"
        case .ready:
            return nil
        }
    }

    public var primaryActionTitle: String {
        isCurrentlySelected ? "Currently Selected" : "Choose This Design"
    }

    public var isPrimaryActionEnabled: Bool {
        !isCurrentlySelected
    }
}
