package infrastructure_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"muse-backend/internal/museum/domain"
	"muse-backend/internal/museum/infrastructure"
)

func TestInsertPhotoSlots_SameAssetTwice_IsAlreadyAssigned(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))
	room, _ := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "R", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	assetID := createCommittedAsset(t, pool, museum.AccountID, "once")
	now := time.Now()

	if err := repo.InsertPhotoSlots(ctx, room.ID, []domain.PhotoSlotAssignment{{SlotIndex: 0, PhotoAssetID: assetID, CreatedAt: now, UpdatedAt: now}}); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := repo.InsertPhotoSlots(ctx, room.ID, []domain.PhotoSlotAssignment{{SlotIndex: 1, PhotoAssetID: assetID, CreatedAt: now, UpdatedAt: now}})

	if !errors.Is(err, domain.ErrPhotoAssetAlreadyAssigned) {
		t.Fatalf("expected ErrPhotoAssetAlreadyAssigned from the unique index, got %v", err)
	}
	var assetErr *domain.PhotoAssetError
	if !errors.As(err, &assetErr) || assetErr.AssetID != assetID {
		t.Errorf("the offending asset must be named; got %v", err)
	}
}

func TestInsertPhotoSlots_SameSlotTwice_IsSlotOccupied(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))
	room, _ := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "R", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	a := createCommittedAsset(t, pool, museum.AccountID, "a")
	b := createCommittedAsset(t, pool, museum.AccountID, "b")
	now := time.Now()

	_ = repo.InsertPhotoSlots(ctx, room.ID, []domain.PhotoSlotAssignment{{SlotIndex: 0, PhotoAssetID: a, CreatedAt: now, UpdatedAt: now}})
	err := repo.InsertPhotoSlots(ctx, room.ID, []domain.PhotoSlotAssignment{{SlotIndex: 0, PhotoAssetID: b, CreatedAt: now, UpdatedAt: now}})

	if !errors.Is(err, domain.ErrSlotOccupied) {
		t.Fatalf("expected ErrSlotOccupied, got %v", err)
	}
}

func TestInsertPhotoSlots_UnknownAsset_IsRefusedByTheDatabase(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))
	room, _ := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "R", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	now := time.Now()

	err := repo.InsertPhotoSlots(ctx, room.ID, []domain.PhotoSlotAssignment{{
		SlotIndex: 0, PhotoAssetID: "00000000-0000-4000-8000-000000000000", CreatedAt: now, UpdatedAt: now,
	}})
	if err == nil {
		t.Fatal("expected the foreign key to refuse a nonexistent asset")
	}
}

func TestAssetReferencedByASlot_CannotBeDeleted(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))
	room, _ := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "R", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	assetID := createCommittedAsset(t, pool, museum.AccountID, "kept")
	now := time.Now()
	_ = repo.InsertPhotoSlots(ctx, room.ID, []domain.PhotoSlotAssignment{{SlotIndex: 0, PhotoAssetID: assetID, CreatedAt: now, UpdatedAt: now}})

	if _, err := pool.Pool().Exec(ctx, `DELETE FROM assets WHERE id = $1`, assetID); err == nil {
		t.Fatal("deleting an asset a slot references must be refused (ON DELETE RESTRICT)")
	}
}

func TestFindPhotoSlotRoomsByAssetIDs_ReportsWhereEachAssetHangs(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))
	roomA, _ := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "A", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	roomB, _ := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "B", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	a := createCommittedAsset(t, pool, museum.AccountID, "a")
	b := createCommittedAsset(t, pool, museum.AccountID, "b")
	unassigned := createCommittedAsset(t, pool, museum.AccountID, "u")
	now := time.Now()
	_ = repo.InsertPhotoSlots(ctx, roomA.ID, []domain.PhotoSlotAssignment{{SlotIndex: 0, PhotoAssetID: a, CreatedAt: now, UpdatedAt: now}})
	_ = repo.InsertPhotoSlots(ctx, roomB.ID, []domain.PhotoSlotAssignment{{SlotIndex: 0, PhotoAssetID: b, CreatedAt: now, UpdatedAt: now}})

	where, err := repo.FindPhotoSlotRoomsByAssetIDs(ctx, []string{a, b, unassigned})
	if err != nil {
		t.Fatalf("FindPhotoSlotRoomsByAssetIDs: %v", err)
	}
	if where[a] != roomA.ID || where[b] != roomB.ID {
		t.Errorf("wrong rooms: %+v", where)
	}
	if _, present := where[unassigned]; present {
		t.Error("an unassigned asset must be absent from the map")
	}
}

func TestLockAndInsert_InsideUnitOfWork_RollsBackTogether(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	museum := newMuseum(t, repo, createAccount(t, pool))
	room, _ := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "R", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	assetID := createCommittedAsset(t, pool, museum.AccountID, "rolled")
	sentinel := errors.New("later step failed")

	err := pool.Run(ctx, func(ctx context.Context) error {
		locked, err := repo.LockRoomForUpdate(ctx, room.ID)
		if err != nil {
			return err
		}
		if len(locked.PhotoSlots) != 0 {
			return errors.New("expected an empty room")
		}
		now := time.Now()
		if err := repo.InsertPhotoSlots(ctx, room.ID, []domain.PhotoSlotAssignment{{SlotIndex: 0, PhotoAssetID: assetID, CreatedAt: now, UpdatedAt: now}}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}

	after, _ := repo.FindRoom(ctx, room.ID)
	if len(after.PhotoSlots) != 0 {
		t.Errorf("the insert must have rolled back, got %d slots", len(after.PhotoSlots))
	}
}
