package domain_test

import (
	"testing"

	"muse-backend/internal/museum/domain"
)

func museum(id domain.MuseumID, owner string, privacy domain.Privacy) domain.Museum {
	return domain.Museum{ID: id, AccountID: owner, Privacy: privacy}
}

func room(id domain.RoomID, in domain.MuseumID, privacy domain.Privacy) domain.Room {
	return domain.Room{ID: id, MuseumID: in, Privacy: privacy}
}

func TestVisibility_VisitorSeesOnlyPublicMuseums(t *testing.T) {
	if !domain.VisitorCanSeeMuseum(museum("m", "owner", domain.PrivacyPublic)) {
		t.Fatal("a Public Museum is visible to visitors")
	}
	if domain.VisitorCanSeeMuseum(museum("m", "owner", domain.PrivacyPrivate)) {
		t.Fatal("a Private Museum is invisible to visitors")
	}
}

func TestVisibility_VisitorRoomTable(t *testing.T) {
	cases := []struct {
		museumPrivacy, roomPrivacy domain.Privacy
		want                       bool
	}{
		{domain.PrivacyPublic, domain.PrivacyPublic, true},
		{domain.PrivacyPublic, domain.PrivacyPrivate, false},
		{domain.PrivacyPrivate, domain.PrivacyPrivate, false},
		{domain.PrivacyPrivate, domain.PrivacyPublic, false},
	}
	for _, c := range cases {
		m := museum("m", "owner", c.museumPrivacy)
		r := room("r", "m", c.roomPrivacy)
		if got := domain.VisitorCanSeeRoom(m, r); got != c.want {
			t.Fatalf("museum %s / room %s: visitor visibility = %v, want %v", c.museumPrivacy, c.roomPrivacy, got, c.want)
		}
	}
}

func TestVisibility_OwnerAlwaysSeesTheirOwn(t *testing.T) {
	for _, mp := range []domain.Privacy{domain.PrivacyPublic, domain.PrivacyPrivate} {
		for _, rp := range []domain.Privacy{domain.PrivacyPublic, domain.PrivacyPrivate} {
			m := museum("m", "owner", mp)
			r := room("r", "m", rp)
			if !domain.MuseumVisibleTo(m, "owner") {
				t.Fatalf("owner must see their %s Museum", mp)
			}
			if !domain.RoomVisibleTo(m, r, "owner") {
				t.Fatalf("owner must see their %s Room in a %s Museum", rp, mp)
			}
		}
	}
}

func TestVisibility_AStrangerIsAVisitor(t *testing.T) {
	m := museum("m", "owner", domain.PrivacyPrivate)
	r := room("r", "m", domain.PrivacyPublic)

	if domain.MuseumVisibleTo(m, "stranger") {
		t.Fatal("a stranger must not see a Private Museum")
	}
	if domain.RoomVisibleTo(m, r, "stranger") {
		t.Fatal("a stranger must not see a Public Room inside a Private Museum ( a)")
	}
	if domain.MuseumVisibleTo(m, "") {
		t.Fatal("an empty caller id is nobody, not the owner")
	}
}

func TestVisibility_RoomMustBelongToTheMuseum(t *testing.T) {
	m := museum("m", "owner", domain.PrivacyPublic)
	other := room("r", "another-museum", domain.PrivacyPublic)

	if domain.VisitorCanSeeRoom(m, other) {
		t.Fatal("a Room from another Museum must not be visible under this one")
	}
	if domain.RoomVisibleTo(m, other, "owner") {
		t.Fatal("not even to this Museum's owner — it is not their Room")
	}
}

func TestVisibility_VisibleRoomsFiltersServerSideAndKeepsOrder(t *testing.T) {
	m := museum("m", "owner", domain.PrivacyPublic)
	rooms := []domain.Room{
		room("a", "m", domain.PrivacyPublic),
		room("b", "m", domain.PrivacyPrivate),
		room("c", "m", domain.PrivacyPublic),
		room("d", "other", domain.PrivacyPublic),
	}

	visitor := domain.VisibleRooms(m, rooms, "stranger")
	owner := domain.VisibleRooms(m, rooms, "owner")

	if len(visitor) != 2 || visitor[0].ID != "a" || visitor[1].ID != "c" {
		t.Fatalf("visitor sees %v, want [a c] in order", visitor)
	}
	if len(owner) != 3 || owner[1].ID != "b" {
		t.Fatalf("owner sees %v, want [a b c] — their Private Room included, the foreign one not", owner)
	}
}

func TestVisibility_VisibleRoomsOfAPrivateMuseumIsEmptyForVisitors(t *testing.T) {
	m := museum("m", "owner", domain.PrivacyPrivate)
	rooms := []domain.Room{room("a", "m", domain.PrivacyPublic), room("b", "m", domain.PrivacyPublic)}

	if got := domain.VisibleRooms(m, rooms, "stranger"); len(got) != 0 {
		t.Fatalf("a Private Museum's Rooms are all hidden from visitors, got %d", len(got))
	}
}

func TestRoomPatch_IsEmpty(t *testing.T) {
	name := "x"
	if !(domain.RoomPatch{}).IsEmpty() {
		t.Fatal("no fields set is empty")
	}
	if (domain.RoomPatch{Name: &name}).IsEmpty() {
		t.Fatal("a set field is not empty")
	}
}
