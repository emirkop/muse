import Foundation

public enum RoomWall: String, Equatable, Sendable, CaseIterable {
    case focal
    case left
    case right
    case rear

    var opposite: RoomWall {
        switch self {
        case .left: return .right
        case .right: return .left
        case .focal, .rear: return self
        }
    }
}

public struct SlotAnchor: Hashable, Sendable {
    public let wall: RoomWall
    public let positionOnWall: Int

    public init(wall: RoomWall, positionOnWall: Int) {
        self.wall = wall
        self.positionOnWall = positionOnWall
    }
}

public struct LogicalPhotoSlot: Equatable, Sendable {
    public let index: Int
    public let anchor: SlotAnchor

    public var wall: RoomWall { anchor.wall }
    public var positionOnWall: Int { anchor.positionOnWall }

    public init(index: Int, anchor: SlotAnchor) {
        self.index = index
        self.anchor = anchor
    }
}

public enum RoomPhotoSlotLayout {
    public static let firstAlternatingWall: RoomWall = .left

    public static func supports(photoCount: Int) -> Bool {
        photoCount >= 0 && photoCount <= Room.maxPhotos
    }

    public static func slots(forPhotoCount photoCount: Int) -> [LogicalPhotoSlot] {
        guard supports(photoCount: photoCount), photoCount > 0 else { return [] }

        var slots = [LogicalPhotoSlot(index: 0, anchor: SlotAnchor(wall: .focal, positionOnWall: 0))]

        let nonFocal = photoCount - 1
        let usesRearWall = nonFocal % 2 == 1
        let sideWallCount = usesRearWall ? nonFocal - 1 : nonFocal

        for sideOrder in 0..<sideWallCount {
            let wall = sideOrder % 2 == 0 ? firstAlternatingWall : firstAlternatingWall.opposite
            let positionOnWall = sideOrder / 2
            slots.append(
                LogicalPhotoSlot(
                    index: sideOrder + 1,
                    anchor: SlotAnchor(wall: wall, positionOnWall: positionOnWall)
                )
            )
        }

        if usesRearWall {
            slots.append(
                LogicalPhotoSlot(
                    index: photoCount - 1,
                    anchor: SlotAnchor(wall: .rear, positionOnWall: 0)
                )
            )
        }

        return slots
    }

    public static var requiredAnchorsForFullRoom: Set<SlotAnchor> {
        Set(slots(forPhotoCount: Room.maxPhotos).map(\.anchor))
    }
}

public enum RoomSculptureSlotLayout {
    public static var slotCount: Int { Room.maxSculptures }

    public static func isValid(slotIndex: Int) -> Bool {
        slotIndex >= 0 && slotIndex < slotCount
    }

    public static func lowestFreeSlot(occupied: [SculptureInstance]) -> Int? {
        let taken = Set(occupied.map(\.slotIndex))
        return (0..<slotCount).first { !taken.contains($0) }
    }
}
