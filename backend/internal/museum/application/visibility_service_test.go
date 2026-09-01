package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/museum/domain"
)

func patchOf(name, variantID string, privacy domain.Privacy) domain.RoomPatch {
	return domain.RoomPatch{Name: &name, VariantID: &variantID, Privacy: &privacy}
}

func privacyPatch(privacy domain.Privacy) domain.RoomPatch {
	return domain.RoomPatch{Privacy: &privacy}
}

func seedOwner(t *testing.T) (*harnessOwner, func()) {
	t.Helper()
	service, repo, catalog := newServiceWithCatalog()
	ctx := context.Background()
	museum, err := service.CreateMuseum(ctx, "owner", "style_modern")
	if err != nil {
		t.Fatalf("create museum: %v", err)
	}
	public, err := service.CreateRoom(ctx, "owner", "Public Hall", "modern_hall")
	if err != nil {
		t.Fatalf("create public room: %v", err)
	}
	if err := service.UpdateRoom(ctx, "owner", public.ID, privacyPatch(domain.PrivacyPublic)); err != nil {
		t.Fatalf("make room public: %v", err)
	}
	private, err := service.CreateRoom(ctx, "owner", "Private Study", "modern_hall")
	if err != nil {
		t.Fatalf("create private room: %v", err)
	}
	return &harnessOwner{service: service, repo: repo, catalog: catalog, museum: museum, public: public, private: private}, func() {}
}

type harnessOwner struct {
	service interface {
		CreateMuseum(ctx context.Context, accountID, styleID string) (domain.Museum, error)
		VisibleMuseum(ctx context.Context, caller string, id domain.MuseumID) (domain.Museum, []domain.Room, error)
		VisibleRoom(ctx context.Context, caller string, museumID domain.MuseumID, roomID domain.RoomID) (domain.Room, error)
		VisitorMuseum(ctx context.Context, id domain.MuseumID) (domain.Museum, []domain.Room, error)
		VisitorRoom(ctx context.Context, museumID domain.MuseumID, roomID domain.RoomID) (domain.Room, error)
		ChangePrivacy(ctx context.Context, accountID string, privacy domain.Privacy) error
		UpdateRoom(ctx context.Context, accountID string, roomID domain.RoomID, patch domain.RoomPatch) error
		FindRoom(ctx context.Context, accountID string, roomID domain.RoomID) (domain.Room, error)
		AssignRoomMusic(ctx context.Context, accountID string, roomID domain.RoomID, trackID string) error
		RemoveRoomMusic(ctx context.Context, accountID string, roomID domain.RoomID) error
	}
	repo    *fakeRepo
	catalog *fakeCatalog
	museum  domain.Museum
	public  domain.Room
	private domain.Room
}

func TestVisibleMuseum_ByID_IsOwnerOnly_RegardlessOfPrivacy(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()

	for _, privacy := range []domain.Privacy{domain.PrivacyPrivate, domain.PrivacyPublic} {
		if err := h.service.ChangePrivacy(ctx, "owner", privacy); err != nil {
			t.Fatal(err)
		}
		if _, _, err := h.service.VisibleMuseum(ctx, "stranger", h.museum.ID); !errors.Is(err, domain.ErrNotVisible) {
			t.Fatalf("%s Museum by id must be invisible to a stranger, got %v", privacy, err)
		}
		if _, err := h.service.VisibleRoom(ctx, "stranger", h.museum.ID, h.public.ID); !errors.Is(err, domain.ErrNotVisible) {
			t.Fatalf("%s Museum's Public Room by id must be invisible to a stranger, got %v", privacy, err)
		}
	}
}

func TestVisitorMuseum_PrivateMuseumIsInvisible_EvenItsPublicRooms(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()

	if _, _, err := h.service.VisitorMuseum(ctx, h.museum.ID); !errors.Is(err, domain.ErrNotVisible) {
		t.Fatalf("a Private Museum must be invisible to a visitor, got %v", err)
	}
	if _, err := h.service.VisitorRoom(ctx, h.museum.ID, h.public.ID); !errors.Is(err, domain.ErrNotVisible) {
		t.Fatalf(" (a): a Public Room in a Private Museum is invisible, got %v", err)
	}
}

func TestVisibleMuseum_OwnerSeesTheirPrivateMuseumWithEveryRoom(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()

	museum, rooms, err := h.service.VisibleMuseum(ctx, "owner", h.museum.ID)
	if err != nil {
		t.Fatalf("owner must see their own Museum: %v", err)
	}
	if museum.ID != h.museum.ID || len(rooms) != 2 {
		t.Fatalf("owner must see both Rooms, got %d", len(rooms))
	}
	if _, err := h.service.VisibleRoom(ctx, "owner", h.museum.ID, h.private.ID); err != nil {
		t.Fatalf("owner must see their Private Room: %v", err)
	}
}

func TestVisitorMuseum_PublicMuseumShowsOnlyItsPublicRooms(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	if err := h.service.ChangePrivacy(ctx, "owner", domain.PrivacyPublic); err != nil {
		t.Fatalf("publish museum: %v", err)
	}

	_, rooms, err := h.service.VisitorMuseum(ctx, h.museum.ID)
	if err != nil {
		t.Fatalf("a Public Museum must be visible to a visitor: %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID != h.public.ID {
		t.Fatalf("visitor must see exactly the Public Room, got %+v", rooms)
	}
	if _, err := h.service.VisitorRoom(ctx, h.museum.ID, h.public.ID); err != nil {
		t.Fatalf("the Public Room must be visible: %v", err)
	}
	if _, err := h.service.VisitorRoom(ctx, h.museum.ID, h.private.ID); !errors.Is(err, domain.ErrNotVisible) {
		t.Fatalf("the Private Room must be invisible, got %v", err)
	}
}

func TestVisibility_EveryRefusalIsErrNotVisible(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	if err := h.service.ChangePrivacy(ctx, "owner", domain.PrivacyPublic); err != nil {
		t.Fatal(err)
	}
	const nonexistent = "00000000-0000-4000-8000-000000000000"

	refusals := map[string]error{}
	_, _, refusals["by id: nonexistent museum"] = h.service.VisibleMuseum(ctx, "stranger", nonexistent)
	_, _, refusals["by id: malformed museum id"] = h.service.VisibleMuseum(ctx, "stranger", "not-an-id")
	_, _, refusals["by id: public museum, stranger"] = h.service.VisibleMuseum(ctx, "stranger", h.museum.ID)
	_, refusals["by id: nonexistent room"] = h.service.VisibleRoom(ctx, "stranger", h.museum.ID, nonexistent)
	_, refusals["by id: malformed room id"] = h.service.VisibleRoom(ctx, "stranger", h.museum.ID, "../../etc")
	_, refusals["by id: private room"] = h.service.VisibleRoom(ctx, "stranger", h.museum.ID, h.private.ID)
	_, refusals["by id: room under wrong museum"] = h.service.VisibleRoom(ctx, "stranger", nonexistent, h.public.ID)
	_, _, refusals["visitor: nonexistent museum"] = h.service.VisitorMuseum(ctx, nonexistent)
	_, _, refusals["visitor: malformed museum id"] = h.service.VisitorMuseum(ctx, "not-an-id")
	_, refusals["visitor: nonexistent room"] = h.service.VisitorRoom(ctx, h.museum.ID, nonexistent)
	_, refusals["visitor: malformed room id"] = h.service.VisitorRoom(ctx, h.museum.ID, "../../etc")
	_, refusals["visitor: private room"] = h.service.VisitorRoom(ctx, h.museum.ID, h.private.ID)
	_, refusals["visitor: room under wrong museum"] = h.service.VisitorRoom(ctx, nonexistent, h.public.ID)

	for name, err := range refusals {
		if !errors.Is(err, domain.ErrNotVisible) {
			t.Errorf("%s: got %v, want ErrNotVisible", name, err)
		}
	}
}

func TestVisitorRoom_MustBelongToTheGivenMuseum(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	if err := h.service.ChangePrivacy(ctx, "owner", domain.PrivacyPublic); err != nil {
		t.Fatal(err)
	}
	other, err := h.service.CreateMuseum(ctx, "other", "style_modern")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.ChangePrivacy(ctx, "other", domain.PrivacyPublic); err != nil {
		t.Fatal(err)
	}

	if _, err := h.service.VisitorRoom(ctx, other.ID, h.public.ID); !errors.Is(err, domain.ErrNotVisible) {
		t.Fatalf("a Room under the wrong Museum must be invisible, got %v", err)
	}
	if _, err := h.service.VisibleRoom(ctx, "other", other.ID, h.public.ID); !errors.Is(err, domain.ErrNotVisible) {
		t.Fatalf("owning a different Museum grants nothing, got %v", err)
	}
}

func TestVisibility_ChangesTakeEffectOnTheNextCall(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()

	if err := h.service.ChangePrivacy(ctx, "owner", domain.PrivacyPublic); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.service.VisitorMuseum(ctx, h.museum.ID); err != nil {
		t.Fatalf("visible after publishing: %v", err)
	}
	if err := h.service.ChangePrivacy(ctx, "owner", domain.PrivacyPrivate); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.service.VisitorMuseum(ctx, h.museum.ID); !errors.Is(err, domain.ErrNotVisible) {
		t.Fatalf("invisible again on the very next call, got %v", err)
	}

	if err := h.service.UpdateRoom(ctx, "owner", h.public.ID, privacyPatch(domain.PrivacyPrivate)); err != nil {
		t.Fatal(err)
	}
	if err := h.service.ChangePrivacy(ctx, "owner", domain.PrivacyPublic); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.VisitorRoom(ctx, h.museum.ID, h.public.ID); !errors.Is(err, domain.ErrNotVisible) {
		t.Fatalf("a Room made Private is invisible on the next call, got %v", err)
	}
}

func TestUpdateRoom_PrivacyOnlyPatchTouchesNothingElse(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	before, _ := h.service.FindRoom(ctx, "owner", h.private.ID)

	if err := h.service.UpdateRoom(ctx, "owner", h.private.ID, privacyPatch(domain.PrivacyPublic)); err != nil {
		t.Fatalf("privacy patch: %v", err)
	}

	after, _ := h.service.FindRoom(ctx, "owner", h.private.ID)
	if after.Privacy != domain.PrivacyPublic {
		t.Fatal("privacy must change")
	}
	if after.Name != before.Name || after.VariantID != before.VariantID {
		t.Fatalf("a privacy-only patch must not touch name/variant: before %+v after %+v", before, after)
	}
}

func TestUpdateRoom_EmptyPatchIsANoOpWithoutAWrite(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	writes := h.repo.roomWrites

	if err := h.service.UpdateRoom(ctx, "owner", h.private.ID, domain.RoomPatch{}); err != nil {
		t.Fatalf("an empty PATCH is a legitimate no-op, got %v", err)
	}
	if h.repo.roomWrites != writes {
		t.Fatal("an empty patch must not write")
	}
}

func TestUpdateRoom_ValidatesOnlyWhatIsSupplied(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	bad := domain.Privacy("unlisted")

	if err := h.service.UpdateRoom(ctx, "owner", h.private.ID, domain.RoomPatch{Privacy: &bad}); !errors.Is(err, domain.ErrInvalidPrivacy) {
		t.Fatalf("an invalid privacy value must be refused, got %v", err)
	}
	wrongStyle := "gothic_hall"
	if err := h.service.UpdateRoom(ctx, "owner", h.private.ID, domain.RoomPatch{VariantID: &wrongStyle}); err == nil {
		t.Fatal("a Variant from another Style must still be refused when supplied")
	}
	if err := h.service.UpdateRoom(ctx, "owner", h.private.ID, privacyPatch(domain.PrivacyPublic)); err != nil {
		t.Fatalf("privacy-only patch must not depend on the Variant: %v", err)
	}
}

func TestUpdateRoom_IntruderIsRefusedWithoutAWrite(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	if _, err := h.service.CreateMuseum(ctx, "intruder-with-museum", "style_modern"); err != nil {
		t.Fatalf("intruder museum: %v", err)
	}
	writes := h.repo.roomWrites

	for caller, want := range map[string]error{
		"intruder-without-museum": domain.ErrMuseumNotFound,
		"intruder-with-museum":    domain.ErrNotOwner,
	} {
		err := h.service.UpdateRoom(ctx, caller, h.private.ID, privacyPatch(domain.PrivacyPublic))
		if !errors.Is(err, want) {
			t.Fatalf("%s: expected %v, got %v", caller, want, err)
		}
	}
	if h.repo.roomWrites != writes {
		t.Fatal("an intruder's patch must not write")
	}
	room, _ := h.service.FindRoom(ctx, "owner", h.private.ID)
	if room.Privacy != domain.PrivacyPrivate {
		t.Fatal("the Room must remain Private")
	}
}

func TestRequireOwnedRoom_MalformedIDIsNotFoundNotAnError(t *testing.T) {
	h, _ := seedOwner(t)

	_, err := h.service.FindRoom(context.Background(), "owner", "not-a-uuid")

	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("a malformed id must read as not found, got %v", err)
	}
}
