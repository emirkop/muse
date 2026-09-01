import Foundation

public struct RoomEditModeState: Equatable, Sendable {
    public let role: RoomViewerRole
    public private(set) var isEditing: Bool

    public init(role: RoomViewerRole) {
        self.role = role
        self.isEditing = false
    }

    public var canEdit: Bool { role == .owner }

    @discardableResult
    public mutating func enter() -> Bool {
        guard canEdit else { return false }
        isEditing = true
        return true
    }

    public mutating func exit() {
        isEditing = false
    }

    public mutating func toggle() {
        if isEditing { exit() } else { enter() }
    }
}
