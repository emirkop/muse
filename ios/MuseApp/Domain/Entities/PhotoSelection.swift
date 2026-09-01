import Foundation

public struct PickedPhoto: Equatable, Sendable, Identifiable {
    public let id: String
    public let assetIdentifier: String?
    public let loadState: PickedPhotoLoadState

    public init(id: String, assetIdentifier: String?, loadState: PickedPhotoLoadState) {
        self.id = id
        self.assetIdentifier = assetIdentifier
        self.loadState = loadState
    }

    public var didLoad: Bool {
        if case .ready = loadState { return true }
        return false
    }
}

public enum PickedPhotoLoadState: Equatable, Sendable {
    case ready(thumbnail: Data, file: NormalizedPhotoFile)
    case failed
}

public extension PickedPhoto {
    var normalizedFile: NormalizedPhotoFile? {
        if case .ready(_, let file) = loadState { return file }
        return nil
    }
}

public extension Room {
    var remainingPhotoCapacity: Int {
        max(0, Self.maxPhotos - photoSlots.count)
    }
}
