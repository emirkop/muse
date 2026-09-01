package domain

type RoomWall string

const (
	WallFocal RoomWall = "focal"
	WallLeft  RoomWall = "left"
	WallRight RoomWall = "right"
	WallRear  RoomWall = "rear"
)

const FirstAlternatingWall = WallLeft

type LogicalPhotoSlot struct {
	Index          int
	Wall           RoomWall
	PositionOnWall int
}

func SupportsPhotoCount(count int) bool {
	return count >= 0 && count <= MaxPhotosPerRoom
}

func PhotoSlotLayout(count int) []LogicalPhotoSlot {
	if !SupportsPhotoCount(count) || count == 0 {
		return nil
	}

	slots := make([]LogicalPhotoSlot, 0, count)
	slots = append(slots, LogicalPhotoSlot{Index: 0, Wall: WallFocal, PositionOnWall: 0})

	nonFocal := count - 1
	usesRearWall := nonFocal%2 == 1
	sideWallCount := nonFocal
	if usesRearWall {
		sideWallCount = nonFocal - 1
	}

	for sideOrder := 0; sideOrder < sideWallCount; sideOrder++ {
		wall := FirstAlternatingWall
		if sideOrder%2 == 1 {
			wall = oppositeWall(FirstAlternatingWall)
		}
		slots = append(slots, LogicalPhotoSlot{
			Index:          sideOrder + 1,
			Wall:           wall,
			PositionOnWall: sideOrder / 2,
		})
	}

	if usesRearWall {
		slots = append(slots, LogicalPhotoSlot{Index: count - 1, Wall: WallRear, PositionOnWall: 0})
	}

	return slots
}

func oppositeWall(w RoomWall) RoomWall {
	switch w {
	case WallLeft:
		return WallRight
	case WallRight:
		return WallLeft
	default:
		return w
	}
}

func LowestFreeSculptureSlot(occupied []SculptureInstance) (int, bool) {
	taken := make(map[int]struct{}, len(occupied))
	for _, sculpture := range occupied {
		taken[sculpture.SlotIndex] = struct{}{}
	}
	for index := 0; index < MaxSculpturesPerRoom; index++ {
		if _, used := taken[index]; !used {
			return index, true
		}
	}
	return 0, false
}
