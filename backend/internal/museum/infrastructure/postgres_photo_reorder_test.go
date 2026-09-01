package infrastructure_test

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/museum/domain"
	"muse-backend/internal/museum/infrastructure"
	"muse-backend/internal/platform/database"
)

type seededRoom struct {
	room   domain.Room
	assets []string
}

func seedRoomWithPhotos(t *testing.T, repo *infrastructure.PostgresMuseumRepository, poolForAssets func(*testing.T, string, string) string, accountID string, n int) seededRoom {
	t.Helper()
	ctx := context.Background()
	museum := newMuseum(t, repo, accountID)
	room, err := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "R", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	now := time.Now()
	assets := make([]string, 0, n)
	slots := make([]domain.PhotoSlotAssignment, 0, n)
	for i := 0; i < n; i++ {
		assetID := poolForAssets(t, accountID, "p"+strings.Repeat("x", i))
		assets = append(assets, assetID)
		slots = append(slots, domain.PhotoSlotAssignment{
			SlotIndex: i, PhotoAssetID: assetID, Caption: "caption for " + assetID, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := repo.InsertPhotoSlots(ctx, room.ID, slots); err != nil {
		t.Fatalf("insert slots: %v", err)
	}
	return seededRoom{room: room, assets: assets}
}

func currentOrder(t *testing.T, repo *infrastructure.PostgresMuseumRepository, roomID domain.RoomID) ([]string, map[string]string) {
	t.Helper()
	room, err := repo.FindRoom(context.Background(), roomID)
	if err != nil {
		t.Fatalf("find room: %v", err)
	}
	order := make([]string, len(room.PhotoSlots))
	captions := make(map[string]string, len(room.PhotoSlots))
	seen := map[int]bool{}
	for _, slot := range room.PhotoSlots {
		if slot.SlotIndex < 0 || slot.SlotIndex >= len(room.PhotoSlots) || seen[slot.SlotIndex] {
			t.Fatalf("indices not contiguous 0..%d: %+v", len(room.PhotoSlots)-1, room.PhotoSlots)
		}
		seen[slot.SlotIndex] = true
		order[slot.SlotIndex] = slot.PhotoAssetID
		captions[slot.PhotoAssetID] = slot.Caption
	}
	return order, captions
}

func TestReorder_NaiveSequentialSwap_StillViolatesUniqueness(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 2)
	ctx := context.Background()

	err := pool.Run(ctx, func(ctx context.Context) error {
		exec := database.ExecutorFrom(ctx, pool.Pool())
		if _, err := exec.Exec(ctx, `UPDATE room_photo_slots SET slot_index = 1 WHERE room_id = $1 AND photo_asset_id = $2`, string(seeded.room.ID), seeded.assets[0]); err != nil {
			return err
		}
		_, err := exec.Exec(ctx, `UPDATE room_photo_slots SET slot_index = 0 WHERE room_id = $1 AND photo_asset_id = $2`, string(seeded.room.ID), seeded.assets[1])
		return err
	})

	if err == nil || !strings.Contains(err.Error(), "room_photo_slots_unique_slot") {
		t.Fatalf("a naive sequential swap must still collide with the unique constraint; got %v", err)
	}
	order, _ := currentOrder(t, repo, seeded.room.ID)
	if order[0] != seeded.assets[0] || order[1] != seeded.assets[1] {
		t.Error("the failed swap must leave the order unchanged")
	}
}

func TestReorder_TwoPhotoSwap_UnderDeferredConstraint(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 2)
	ctx := context.Background()

	err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		return repo.ReorderPhotoSlots(ctx, seeded.room.ID, []string{seeded.assets[1], seeded.assets[0]})
	})
	if err != nil {
		t.Fatalf("reorder: %v", err)
	}

	order, captions := currentOrder(t, repo, seeded.room.ID)
	if order[0] != seeded.assets[1] || order[1] != seeded.assets[0] {
		t.Fatalf("swap not applied: %v", order)
	}
	for asset, caption := range captions {
		if caption != "caption for "+asset {
			t.Errorf("caption detached from its photograph: %s → %q", asset, caption)
		}
	}
}

func TestReorder_ArbitraryPermutationOf28_LeavesContiguousIndices_AndSameRows(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), domain.MaxPhotosPerRoom)
	ctx := context.Background()

	idsBefore := map[string]string{}
	rows, _ := pool.Pool().Query(ctx, `SELECT id, photo_asset_id FROM room_photo_slots WHERE room_id = $1`, string(seeded.room.ID))
	for rows.Next() {
		var id, asset string
		_ = rows.Scan(&id, &asset)
		idsBefore[asset] = id
	}
	rows.Close()

	want := append([]string(nil), seeded.assets...)
	rand.New(rand.NewSource(2038)).Shuffle(len(want), func(i, j int) { want[i], want[j] = want[j], want[i] })

	err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		return repo.ReorderPhotoSlots(ctx, seeded.room.ID, want)
	})
	if err != nil {
		t.Fatalf("reorder: %v", err)
	}

	order, _ := currentOrder(t, repo, seeded.room.ID)
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("slot %d holds %s, want %s", i, order[i], want[i])
		}
	}
	rows, _ = pool.Pool().Query(ctx, `SELECT id, photo_asset_id FROM room_photo_slots WHERE room_id = $1`, string(seeded.room.ID))
	for rows.Next() {
		var id, asset string
		_ = rows.Scan(&id, &asset)
		if idsBefore[asset] != id {
			t.Errorf("row identity changed for %s", asset)
		}
	}
	rows.Close()
}

func TestReorder_CollisionAtCommit_RollsBackToThePreviousOrder(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 3)
	ctx := context.Background()
	before, _ := currentOrder(t, repo, seeded.room.ID)

	err := pool.Run(ctx, func(ctx context.Context) error {
		exec := database.ExecutorFrom(ctx, pool.Pool())
		if _, err := exec.Exec(ctx, `SET CONSTRAINTS room_photo_slots_unique_slot DEFERRED`); err != nil {
			return err
		}
		_, err := exec.Exec(ctx, `UPDATE room_photo_slots SET slot_index = 0 WHERE room_id = $1 AND photo_asset_id = $2`, string(seeded.room.ID), seeded.assets[2])
		return err
	})

	if err == nil || !strings.Contains(err.Error(), "room_photo_slots_unique_slot") {
		t.Fatalf("a duplicate final index must be refused at commit by the deferred constraint; got %v", err)
	}
	after, captions := currentOrder(t, repo, seeded.room.ID)
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("order changed despite the failed commit: %v → %v", before, after)
		}
	}
	for asset, caption := range captions {
		if caption != "caption for "+asset {
			t.Errorf("caption changed despite rollback: %s → %q", asset, caption)
		}
	}
}

func TestReorder_ForeignAssetInList_IsAMismatch_AndRollsBack(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	account := createAccount(t, pool)
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, account, 3)
	foreign := createCommittedAsset(t, pool, account, "foreign")
	ctx := context.Background()
	before, _ := currentOrder(t, repo, seeded.room.ID)

	err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		return repo.ReorderPhotoSlots(ctx, seeded.room.ID, []string{seeded.assets[1], foreign, seeded.assets[0]})
	})

	if !errors.Is(err, domain.ErrPhotoOrderMismatch) {
		t.Fatalf("expected ErrPhotoOrderMismatch, got %v", err)
	}
	after, _ := currentOrder(t, repo, seeded.room.ID)
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("order changed despite rollback: %v → %v", before, after)
		}
	}
}
