package infrastructure_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/museum/domain"
	"muse-backend/internal/museum/infrastructure"
	"muse-backend/internal/platform/database"
)

type slotRow struct {
	id, assetID, caption string
	slotIndex            int
}

func readSlotRow(t *testing.T, pool *database.Pool, roomID domain.RoomID, assetID string) slotRow {
	t.Helper()
	var row slotRow
	err := pool.Pool().QueryRow(context.Background(),
		`SELECT id::text, photo_asset_id::text, caption, slot_index FROM room_photo_slots WHERE room_id = $1 AND photo_asset_id = $2`,
		string(roomID), assetID,
	).Scan(&row.id, &row.assetID, &row.caption, &row.slotIndex)
	if err != nil {
		t.Fatalf("read slot row for %s: %v", assetID, err)
	}
	return row
}

func TestReplacePhotoSlotAsset_ChangesOnlyTheAssetReference(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	account := createAccount(t, pool)
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, account, 4)
	replacement := createCommittedAsset(t, pool, account, "replacement")
	before := readSlotRow(t, pool, seeded.room.ID, seeded.assets[2])
	orderBefore, _ := currentOrder(t, repo, seeded.room.ID)

	if err := repo.ReplacePhotoSlotAsset(ctx, seeded.room.ID, seeded.assets[2], replacement); err != nil {
		t.Fatalf("ReplacePhotoSlotAsset: %v", err)
	}

	after := readSlotRow(t, pool, seeded.room.ID, replacement)
	if after.id != before.id {
		t.Error("row identity changed — a replacement must update the row, not recreate it")
	}
	if after.slotIndex != before.slotIndex {
		t.Errorf("slot index moved %d → %d — position must be preserved", before.slotIndex, after.slotIndex)
	}
	if after.caption != before.caption {
		t.Errorf("caption changed %q → %q — requires it preserved", before.caption, after.caption)
	}
	if after.assetID != replacement {
		t.Errorf("asset reference = %s, want %s", after.assetID, replacement)
	}

	orderAfter, _ := currentOrder(t, repo, seeded.room.ID)
	for i := range orderBefore {
		want := orderBefore[i]
		if i == before.slotIndex {
			want = replacement
		}
		if orderAfter[i] != want {
			t.Errorf("slot %d holds %s, want %s", i, orderAfter[i], want)
		}
	}
}

func TestReplacePhotoSlotAsset_UnknownPhotograph_IsNotInRoom(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	account := createAccount(t, pool)
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, account, 2)
	replacement := createCommittedAsset(t, pool, account, "replacement")

	err := repo.ReplacePhotoSlotAsset(context.Background(), seeded.room.ID, "00000000-0000-4000-8000-000000000000", replacement)

	if !errors.Is(err, domain.ErrPhotoNotInRoom) {
		t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
	}
}

func TestReplacePhotoSlotAsset_ReplacementAlreadyHanging_IsAlreadyAssigned(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	account := createAccount(t, pool)
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, account, 3)
	before, _ := currentOrder(t, repo, seeded.room.ID)

	err := repo.ReplacePhotoSlotAsset(ctx, seeded.room.ID, seeded.assets[0], seeded.assets[2])
	if !errors.Is(err, domain.ErrPhotoAssetAlreadyAssigned) {
		t.Fatalf("expected ErrPhotoAssetAlreadyAssigned for a photograph already in this Room, got %v", err)
	}
	var assetErr *domain.PhotoAssetError
	if !errors.As(err, &assetErr) || assetErr.AssetID != seeded.assets[2] {
		t.Errorf("the offending asset must be named; got %v", err)
	}

	museum, _ := repo.FindMuseumByAccount(ctx, account)
	other, err := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "B", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	if err != nil {
		t.Fatalf("create other room: %v", err)
	}
	elsewhere := createCommittedAsset(t, pool, account, "elsewhere")
	if err := repo.InsertPhotoSlots(ctx, other.ID, []domain.PhotoSlotAssignment{{SlotIndex: 0, PhotoAssetID: elsewhere}}); err != nil {
		t.Fatalf("insert into other room: %v", err)
	}
	err = repo.ReplacePhotoSlotAsset(ctx, seeded.room.ID, seeded.assets[0], elsewhere)
	if !errors.Is(err, domain.ErrPhotoAssetAlreadyAssigned) {
		t.Fatalf("expected ErrPhotoAssetAlreadyAssigned for a photograph hanging in another Room, got %v", err)
	}

	after, _ := currentOrder(t, repo, seeded.room.ID)
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("a refused replacement must change nothing: %v → %v", before, after)
		}
	}
}

func TestReplacePhotoSlotAsset_UnknownReplacementAsset_IsNotFound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	account := createAccount(t, pool)
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, account, 2)

	err := repo.ReplacePhotoSlotAsset(context.Background(), seeded.room.ID, seeded.assets[0], "00000000-0000-4000-8000-000000000000")

	if !errors.Is(err, domain.ErrPhotoAssetNotFound) {
		t.Fatalf("expected ErrPhotoAssetNotFound from the foreign key, got %v", err)
	}
	if _, captions := currentOrder(t, repo, seeded.room.ID); captions[seeded.assets[0]] == "" {
		t.Error("the original photograph must still hang in the Room")
	}
}

func TestReplacePhotoSlotAsset_CannotReachAnotherRoomsPhotograph(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	account := createAccount(t, pool)
	roomA := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, account, 2)
	museum, _ := repo.FindMuseumByAccount(ctx, account)
	roomB, err := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "B", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	if err != nil {
		t.Fatalf("create room B: %v", err)
	}
	replacement := createCommittedAsset(t, pool, account, "replacement")

	err = repo.ReplacePhotoSlotAsset(ctx, roomB.ID, roomA.assets[0], replacement)

	if !errors.Is(err, domain.ErrPhotoNotInRoom) {
		t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
	}
	order, _ := currentOrder(t, repo, roomA.room.ID)
	if order[0] != roomA.assets[0] {
		t.Error("a photograph was replaced across a Room boundary")
	}
}

func TestReplacePhotoSlotAsset_InsideAFailedTransaction_RollsBack(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	account := createAccount(t, pool)
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, account, 3)
	replacement := createCommittedAsset(t, pool, account, "replacement")
	before, captionsBefore := currentOrder(t, repo, seeded.room.ID)
	sentinel := errors.New("later step failed")

	err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		if err := repo.ReplacePhotoSlotAsset(ctx, seeded.room.ID, seeded.assets[1], replacement); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}

	after, captionsAfter := currentOrder(t, repo, seeded.room.ID)
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("order changed despite rollback: %v → %v", before, after)
		}
	}
	for asset, caption := range captionsBefore {
		if captionsAfter[asset] != caption {
			t.Errorf("caption for %s changed despite rollback", asset)
		}
	}
}
