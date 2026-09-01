import Foundation

public struct Avatar: Equatable, Sendable {
    public let id: String
    public let displayName: String

    public init(id: String, displayName: String) {
        self.id = id
        self.displayName = displayName
    }
}

public enum AvatarCatalog {
    public static let all: [Avatar] = [
        Avatar(id: "avatar_1", displayName: "Avatar 1"),
        Avatar(id: "avatar_2", displayName: "Avatar 2"),
        Avatar(id: "avatar_3", displayName: "Avatar 3"),
        Avatar(id: "avatar_4", displayName: "Avatar 4"),
        Avatar(id: "avatar_5", displayName: "Avatar 5")
    ]
}
