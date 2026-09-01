import Foundation

@MainActor
public final class RoomCreationViewModel {
    public private(set) var onStateChange: (() -> Void)?

    public func setStateChangeHandler(_ handler: @escaping () -> Void) {
        onStateChange = handler
    }

    public private(set) var nameValidationMessage: String?

    public private(set) var name: String = ""

    public init() {}

    public func updateName(_ newName: String) {
        name = newName
        if nameValidationMessage != nil, RoomNamingRules.rejection(for: newName) == nil {
            nameValidationMessage = nil
            onStateChange?()
        }
    }

    public var characterCountText: String {
        "\(name.count)/\(RoomNamingRules.interimMaximumLength)"
    }

    public var isOverLengthLimit: Bool {
        name.count > RoomNamingRules.interimMaximumLength
    }

    public func confirmedName() -> String? {
        if let rejection = RoomNamingRules.rejection(for: name) {
            nameValidationMessage = RoomNamingRules.message(for: rejection)
            onStateChange?()
            return nil
        }
        nameValidationMessage = nil
        return name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    public static let zeroPhotoRoomsGateNotice =
        "You can add photos to this Room later. Whether a Room can stay empty permanently isn't settled yet."

    public static let roomCountCap: Int? = nil
}
