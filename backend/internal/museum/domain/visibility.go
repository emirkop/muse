package domain

func VisitorCanSeeMuseum(museum Museum) bool {
	return museum.Privacy == PrivacyPublic
}

func VisitorCanSeeRoom(museum Museum, room Room) bool {
	return room.MuseumID == museum.ID &&
		VisitorCanSeeMuseum(museum) &&
		room.Privacy == PrivacyPublic
}

func MuseumVisibleTo(museum Museum, callerAccountID string) bool {
	return museum.AccountID == callerAccountID || VisitorCanSeeMuseum(museum)
}

func RoomVisibleTo(museum Museum, room Room, callerAccountID string) bool {
	if room.MuseumID != museum.ID {
		return false
	}
	return museum.AccountID == callerAccountID || VisitorCanSeeRoom(museum, room)
}

func VisibleRooms(museum Museum, rooms []Room, callerAccountID string) []Room {
	visible := make([]Room, 0, len(rooms))
	for _, room := range rooms {
		if RoomVisibleTo(museum, room, callerAccountID) {
			visible = append(visible, room)
		}
	}
	return visible
}

type RoomPatch struct {
	Name      *string
	VariantID *string
	Privacy   *Privacy
}

func (p RoomPatch) IsEmpty() bool {
	return p.Name == nil && p.VariantID == nil && p.Privacy == nil
}
