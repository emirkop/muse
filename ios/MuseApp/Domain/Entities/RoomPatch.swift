import Foundation

public struct RoomPatch: Equatable, Sendable {
    public let name: String?
    public let variantID: String?
    public let privacy: MusePrivacy?

    public init(name: String? = nil, variantID: String? = nil, privacy: MusePrivacy? = nil) {
        self.name = name
        self.variantID = variantID
        self.privacy = privacy
    }

    public static func privacy(_ privacy: MusePrivacy) -> RoomPatch {
        RoomPatch(privacy: privacy)
    }

    public static func variant(_ variantID: String) -> RoomPatch {
        RoomPatch(variantID: variantID)
    }

    public var isEmpty: Bool {
        name == nil && variantID == nil && privacy == nil
    }
}
