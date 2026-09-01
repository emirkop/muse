import CoreGraphics
import Foundation

public struct DecodedPhotoImage: @unchecked Sendable {
    public let image: CGImage
    public let pixelWidth: Int
    public let pixelHeight: Int

    public init(image: CGImage) {
        self.image = image
        self.pixelWidth = image.width
        self.pixelHeight = image.height
    }
}

public enum RoomPhotoLoadFailure: Equatable, Sendable {
    case noTicket
    case ticketRejected
    case download
    case decode
    case cancelled
}

public enum RoomPhotoTextureEvent: Sendable {
    case dimensions(slotIndex: Int, pixelWidth: Int, pixelHeight: Int)
    case decoded(slotIndex: Int, image: DecodedPhotoImage)
    case failed(slotIndex: Int, reason: RoomPhotoLoadFailure)
}

public protocol RoomPhotoTextureProviding: Sendable {
    func textures(
        for placements: [ResolvedPhotoPlacement],
        roomID: String,
        accessToken: String,
        maxLongEdge: Int
    ) -> AsyncStream<RoomPhotoTextureEvent>
}
