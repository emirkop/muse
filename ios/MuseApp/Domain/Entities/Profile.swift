import Foundation

public struct Profile: Equatable, Sendable {
    public let displayName: String
    public let avatarID: String

    public init(displayName: String, avatarID: String) {
        self.displayName = displayName
        self.avatarID = avatarID
    }
}
