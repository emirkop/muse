package infrastructure_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"muse-backend/internal/museum/domain"
	"muse-backend/internal/museum/infrastructure"
)

func TestUpdatePhotoCaption_ChangesOnlyTheCaption(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 4)

	type rowState struct {
		id, assetID, caption string
		slotIndex            int
	}
	var before rowState
	err := pool.Pool().QueryRow(ctx,
		`SELECT id::text, photo_asset_id::text, caption, slot_index FROM room_photo_slots WHERE room_id = $1 AND photo_asset_id = $2`,
		string(seeded.room.ID), seeded.assets[2],
	).Scan(&before.id, &before.assetID, &before.caption, &before.slotIndex)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	if err := repo.UpdatePhotoCaption(ctx, seeded.room.ID, seeded.assets[2], "Trabzon, 1998"); err != nil {
		t.Fatalf("UpdatePhotoCaption: %v", err)
	}

	var after rowState
	err = pool.Pool().QueryRow(ctx,
		`SELECT id::text, photo_asset_id::text, caption, slot_index FROM room_photo_slots WHERE room_id = $1 AND photo_asset_id = $2`,
		string(seeded.room.ID), seeded.assets[2],
	).Scan(&after.id, &after.assetID, &after.caption, &after.slotIndex)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}

	if after.caption != "Trabzon, 1998" {
		t.Errorf("caption = %q", after.caption)
	}
	if after.id != before.id {
		t.Error("row identity changed — a caption edit must not replace the row")
	}
	if after.assetID != before.assetID {
		t.Error("photo identity changed")
	}
	if after.slotIndex != before.slotIndex {
		t.Error("slot index changed — a caption edit must not move a photograph")
	}

	order, _ := currentOrder(t, repo, seeded.room.ID)
	for i, assetID := range seeded.assets {
		if order[i] != assetID {
			t.Fatalf("ordering changed at %d: %v", i, order)
		}
	}
}

func TestUpdatePhotoCaption_ClearingStoresTheEmptyString(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 2)

	if err := repo.UpdatePhotoCaption(ctx, seeded.room.ID, seeded.assets[0], ""); err != nil {
		t.Fatalf("clear: %v", err)
	}

	_, captions := currentOrder(t, repo, seeded.room.ID)
	if captions[seeded.assets[0]] != "" {
		t.Errorf("expected the no-caption state, got %q", captions[seeded.assets[0]])
	}
	if captions[seeded.assets[1]] == "" {
		t.Error("clearing one caption must not clear another")
	}
}

func TestPhotoCaption_NoCaptionIsEmptyStringNeverNull(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 2)
	_ = repo.UpdatePhotoCaption(ctx, seeded.room.ID, seeded.assets[0], "")

	var nulls int
	if err := pool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM room_photo_slots WHERE room_id = $1 AND caption IS NULL`, string(seeded.room.ID),
	).Scan(&nulls); err != nil {
		t.Fatalf("count nulls: %v", err)
	}
	if nulls != 0 {
		t.Errorf("found %d NULL captions; the no-caption state must be ''", nulls)
	}

	if _, err := pool.Pool().Exec(ctx,
		`UPDATE room_photo_slots SET caption = NULL WHERE room_id = $1`, string(seeded.room.ID),
	); err == nil {
		t.Error("the database must refuse a NULL caption")
	}
}

func TestUpdatePhotoCaption_UnknownAsset_IsRefused(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 2)

	err := repo.UpdatePhotoCaption(ctx, seeded.room.ID, "00000000-0000-4000-8000-000000000000", "x")

	if !errors.Is(err, domain.ErrPhotoNotInRoom) {
		t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
	}
}

func TestUpdatePhotoCaption_CannotReachAnotherRoomsPhotograph(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	account := createAccount(t, pool)
	roomA := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, account, 2)

	museum, err := repo.FindMuseumByAccount(ctx, account)
	if err != nil {
		t.Fatalf("find museum: %v", err)
	}
	roomB, err := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "B", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	if err != nil {
		t.Fatalf("create room B: %v", err)
	}

	err = repo.UpdatePhotoCaption(ctx, roomB.ID, roomA.assets[0], "wrong room")

	if !errors.Is(err, domain.ErrPhotoNotInRoom) {
		t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
	}
	_, captions := currentOrder(t, repo, roomA.room.ID)
	if captions[roomA.assets[0]] == "wrong room" {
		t.Error("a caption was written across a Room boundary")
	}
}

func TestUpdatePhotoCaption_StoresUnicodeAndTheBoundExactly(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 2)

	for _, caption := range []string{
		"Trabzon — 1998 📷",
		"İstanbul, Türkiye",
		strings.Repeat("x", domain.MaxCaptionBytes),
	} {
		if err := repo.UpdatePhotoCaption(ctx, seeded.room.ID, seeded.assets[0], caption); err != nil {
			t.Fatalf("%q: %v", caption[:min(20, len(caption))], err)
		}
		_, captions := currentOrder(t, repo, seeded.room.ID)
		if captions[seeded.assets[0]] != caption {
			t.Errorf("caption round-trip changed the text")
		}
	}
}

func TestUpdatePhotoCaption_InsideAFailedTransaction_RollsBack(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 3)
	_, captionsBefore := currentOrder(t, repo, seeded.room.ID)
	sentinel := errors.New("later step failed")

	err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		if err := repo.UpdatePhotoCaption(ctx, seeded.room.ID, seeded.assets[1], "should not survive"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}

	_, captionsAfter := currentOrder(t, repo, seeded.room.ID)
	for asset, caption := range captionsBefore {
		if captionsAfter[asset] != caption {
			t.Errorf("caption for %s changed despite rollback: %q → %q", asset, caption, captionsAfter[asset])
		}
	}
}
