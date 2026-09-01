import Foundation

@MainActor
public final class RoomSculptureEditCoordinator {
    public private(set) var sculptures: [SculptureInstance]

    public var onSculpturesChanged: (([SculptureInstance]) -> Void)?

    private let roomID: String
    private let service: any MuseumServicing
    private let accessToken: String

    private var serverSculptures: [SculptureInstance]
    private var isBusy = false
    private var isActive = true

    public init(
        roomID: String,
        sculptures: [SculptureInstance],
        service: any MuseumServicing,
        accessToken: String
    ) {
        let normalised = RoomSculptures.sorted(sculptures)
        self.roomID = roomID
        self.sculptures = normalised
        self.serverSculptures = normalised
        self.service = service
        self.accessToken = accessToken
    }

    public var isEditing: Bool { isBusy }

    public var hasCapacity: Bool {
        RoomSculptureSlotLayout.lowestFreeSlot(occupied: sculptures) != nil
    }

    public var remainingCapacity: Int { max(0, RoomSculptureSlotLayout.slotCount - sculptures.count) }

    // MARK: - Adding

    public func add(catalogID: String) async -> SculptureEditOutcome {
        guard isActive else { return .rejected(message: Self.unavailableMessage) }
        guard !isBusy else { return .rejected(message: Self.busyMessage) }
        guard !catalogID.isEmpty else { return .rejected(message: Self.unknownSculptureMessage) }
        guard let optimistic = RoomSculptures.adding(catalogID, to: sculptures) else {
            return .rejected(message: Self.fullMessage)
        }

        isBusy = true
        defer { isBusy = false }
        apply(optimistic)

        do {
            let authoritative = try await service.addSculpture(accessToken: accessToken, roomID: roomID, catalogID: catalogID)
            guard isActive else { return .applied(sculptures: authoritative) }
            confirm(authoritative)
            return .applied(sculptures: sculptures)
        } catch {
            guard isActive else { return .failed(message: Self.addFailedMessage) }
            apply(serverSculptures)
            return .failed(message: Self.message(for: error, adding: true))
        }
    }

    // MARK: - Removing

    public func remove(slotIndex: Int) async -> SculptureEditOutcome {
        guard isActive else { return .rejected(message: Self.unavailableMessage) }
        guard !isBusy else { return .rejected(message: Self.busyMessage) }
        guard RoomSculptureSlotLayout.isValid(slotIndex: slotIndex),
              RoomSculptures.isOccupied(slotIndex: slotIndex, in: sculptures) else {
            return .rejected(message: Self.emptySlotMessage)
        }

        isBusy = true
        defer { isBusy = false }
        apply(RoomSculptures.removing(slotIndex: slotIndex, from: sculptures))

        do {
            let authoritative = try await service.removeSculpture(accessToken: accessToken, roomID: roomID, slotIndex: slotIndex)
            guard isActive else { return .applied(sculptures: authoritative) }
            confirm(authoritative)
            return .applied(sculptures: sculptures)
        } catch let error as PhotoAPIError where error.code == "sculpture_not_in_room" {
            guard isActive else { return .applied(sculptures: sculptures) }
            confirm(sculptures)
            return .applied(sculptures: sculptures)
        } catch {
            guard isActive else { return .failed(message: Self.removeFailedMessage) }
            apply(serverSculptures)
            return .failed(message: Self.message(for: error, adding: false))
        }
    }

    public func deactivate() {
        isActive = false
        onSculpturesChanged = nil
    }

    // MARK: - Internals

    private func apply(_ next: [SculptureInstance]) {
        let sorted = RoomSculptures.sorted(next)
        guard sorted != sculptures else { return }
        sculptures = sorted
        onSculpturesChanged?(sorted)
    }

    private func confirm(_ authoritative: [SculptureInstance]) {
        serverSculptures = RoomSculptures.sorted(authoritative)
        apply(serverSculptures)
    }

    private static func message(for error: Error, adding: Bool) -> String {
        guard let apiError = error as? PhotoAPIError else {
            return adding ? addFailedMessage : removeFailedMessage
        }
        switch apiError.code {
        case "sculpture_capacity_reached": return fullMessage
        case "unknown_sculpture": return unknownSculptureMessage
        case "sculpture_not_in_room": return emptySlotMessage
        default: return adding ? addFailedMessage : removeFailedMessage
        }
    }

    // MARK: - Copy

    public static let unavailableMessage = "Sculptures can't be changed right now."
    public static let busyMessage = "One moment — the last sculpture change is still saving."
    public static let fullMessage = "This Room already holds all 3 sculptures. Remove one to add another."
    public static let emptySlotMessage = "That sculpture is no longer in this Room."
    public static let unknownSculptureMessage = "That sculpture isn't available."
    public static let addFailedMessage = "Couldn't add the sculpture. The Room is unchanged."
    public static let removeFailedMessage = "Couldn't remove the sculpture. It's back where it was."

    public var capacityMessage: String {
        switch remainingCapacity {
        case 0: return Self.fullMessage
        case 1: return "You can add 1 more sculpture."
        default: return "You can add up to \(remainingCapacity) more sculptures."
        }
    }
}
