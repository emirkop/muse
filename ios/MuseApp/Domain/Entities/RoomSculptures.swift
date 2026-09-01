import Foundation

public enum RoomSculptures {
    public static func sorted(_ sculptures: [SculptureInstance]) -> [SculptureInstance] {
        sculptures.sorted { $0.slotIndex < $1.slotIndex }
    }

    public static func adding(_ catalogID: String, to sculptures: [SculptureInstance]) -> [SculptureInstance]? {
        guard let slot = RoomSculptureSlotLayout.lowestFreeSlot(occupied: sculptures) else { return nil }
        return sorted(sculptures + [SculptureInstance(slotIndex: slot, catalogID: catalogID)])
    }

    public static func removing(slotIndex: Int, from sculptures: [SculptureInstance]) -> [SculptureInstance] {
        sorted(sculptures.filter { $0.slotIndex != slotIndex })
    }

    public static func isOccupied(slotIndex: Int, in sculptures: [SculptureInstance]) -> Bool {
        sculptures.contains { $0.slotIndex == slotIndex }
    }
}

public extension Room {
    func replacingSculptures(_ sculptures: [SculptureInstance]) -> Room {
        Room(
            id: id,
            name: name,
            variantID: variantID,
            privacy: privacy,
            photoSlots: photoSlots,
            sculptures: sculptures
        )
    }
}

public enum SculptureEditOutcome: Equatable, Sendable {
    case applied(sculptures: [SculptureInstance])
    case rejected(message: String)
    case failed(message: String)
}
