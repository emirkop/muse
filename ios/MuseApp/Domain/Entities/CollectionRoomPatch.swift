import Foundation

public struct CollectionRoomPatch: Equatable, Sendable {
    public let name: String?
    public let categoryID: String?
    public let designID: String?

    public init(name: String? = nil, categoryID: String? = nil, designID: String? = nil) {
        self.name = name
        self.categoryID = categoryID
        self.designID = designID
    }

    public static func name(_ name: String) -> CollectionRoomPatch {
        CollectionRoomPatch(name: name)
    }

    public static func category(_ categoryID: String) -> CollectionRoomPatch {
        CollectionRoomPatch(categoryID: categoryID)
    }

    public static func design(_ designID: String) -> CollectionRoomPatch {
        CollectionRoomPatch(designID: designID)
    }

    public var isEmpty: Bool {
        name == nil && categoryID == nil && designID == nil
    }
}
