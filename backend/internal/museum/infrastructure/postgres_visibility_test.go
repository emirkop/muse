package infrastructure_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/museum/domain"
	"muse-backend/internal/museum/infrastructure"
)

func TestIsPlausibleUUID(t *testing.T) {
	good := []string{"6f1c2c1e-9e4a-4a5b-8c3d-1f2e3a4b5c6d", "00000000-0000-0000-0000-000000000000", "ABCDEF12-3456-7890-ABCD-EF1234567890"}
	bad := []string{"", "not-an-id", "6f1c2c1e9e4a4a5b8c3d1f2e3a4b5c6d", "6f1c2c1e-9e4a-4a5b-8c3d-1f2e3a4b5c6d'", "../x", "6f1c2c1e-9e4a-4a5b-8c3d-1f2e3a4b5c6d0"}
	for _, id := range good {
		if !infrastructure.IsPlausibleUUID(id) {
			t.Fatalf("%q should be plausible", id)
		}
	}
	for _, id := range bad {
		if infrastructure.IsPlausibleUUID(id) {
			t.Fatalf("%q should not be plausible", id)
		}
	}
}

func TestFindMuseumByID_FindsAnyAccountsMuseum(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))

	found, err := repo.FindMuseumByID(ctx, museum.ID)
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if found.ID != museum.ID || found.AccountID != museum.AccountID {
		t.Fatalf("found %+v, want %+v", found, museum)
	}
}

func TestFindMuseumByID_MissingAndMalformedAreBothNotFound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()

	for name, id := range map[string]domain.MuseumID{
		"missing":   "00000000-0000-4000-8000-000000000000",
		"malformed": "not-an-id",
		"injection": "' OR 1=1 --",
	} {
		if _, err := repo.FindMuseumByID(ctx, id); !errors.Is(err, domain.ErrMuseumNotFound) {
			t.Fatalf("%s: got %v, want ErrMuseumNotFound", name, err)
		}
	}
}

func TestFindRoom_MalformedIDIsNotFoundNotADatabaseError(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())

	for _, id := range []domain.RoomID{"not-an-id", "", "../etc/passwd"} {
		if _, err := repo.FindRoom(context.Background(), id); !errors.Is(err, domain.ErrRoomNotFound) {
			t.Fatalf("%q: got %v, want ErrRoomNotFound (a 500 here would confirm the id's shape)", id, err)
		}
	}
}

func TestUpdateRoom_PrivacyOnlyPatchLeavesNameAndVariantUntouched(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))
	created, err := repo.CreateRoom(ctx, domain.Room{
		MuseumID: museum.ID, Name: "Original Name", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate,
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	public := domain.PrivacyPublic
	if err := repo.UpdateRoom(ctx, created.ID, domain.RoomPatch{Privacy: &public}); err != nil {
		t.Fatalf("privacy patch: %v", err)
	}

	after, err := repo.FindRoom(ctx, created.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if after.Privacy != domain.PrivacyPublic {
		t.Fatal("privacy must change")
	}
	if after.Name != "Original Name" || after.VariantID != "style_modern_variant_Hall" {
		t.Fatalf("name/variant must be untouched by a privacy-only patch: %+v", after)
	}
}

func TestUpdateRoom_NameOnlyPatchLeavesPrivacyUntouched(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))
	created, err := repo.CreateRoom(ctx, domain.Room{
		MuseumID: museum.ID, Name: "Before", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPublic,
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	renamed := "After"
	if err := repo.UpdateRoom(ctx, created.ID, domain.RoomPatch{Name: &renamed}); err != nil {
		t.Fatalf("name patch: %v", err)
	}

	after, _ := repo.FindRoom(ctx, created.ID)
	if after.Name != "After" || after.Privacy != domain.PrivacyPublic || after.VariantID != "style_modern_variant_Hall" {
		t.Fatalf("only the name may move: %+v", after)
	}
}

func TestUpdateRoom_UnknownRoomIsNotFound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	public := domain.PrivacyPublic

	err := repo.UpdateRoom(context.Background(), "00000000-0000-4000-8000-000000000000", domain.RoomPatch{Privacy: &public})

	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("got %v, want ErrRoomNotFound", err)
	}
}
