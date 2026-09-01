package infrastructure_test

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	"muse-backend/internal/museum/domain"
	"muse-backend/internal/museum/infrastructure"
	"muse-backend/internal/platform/database"
)

func rowIDsByAsset(t *testing.T, pool *database.Pool, roomID domain.RoomID) map[string]string {
	t.Helper()
	rows, err := pool.Pool().Query(context.Background(),
		`SELECT id::text, photo_asset_id::text FROM room_photo_slots WHERE room_id = $1`, string(roomID))
	if err != nil {
		t.Fatalf("row ids: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, asset string
		if err := rows.Scan(&id, &asset); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[asset] = id
	}
	return out
}

func TestDeletePhotoSlotCompacting_RemovesTheRow_AndClosesTheGap(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 5)
	idsBefore := rowIDsByAsset(t, pool, seeded.room.ID)

	err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		return repo.DeletePhotoSlotCompacting(ctx, seeded.room.ID, seeded.assets[2])
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	order, captions := currentOrder(t, repo, seeded.room.ID)
	want := []string{seeded.assets[0], seeded.assets[1], seeded.assets[3], seeded.assets[4]}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("slot %d holds %s, want %s", i, order[i], want[i])
		}
		if captions[want[i]] != "caption for "+want[i] {
			t.Errorf("caption detached from %s: %q", want[i], captions[want[i]])
		}
	}
	idsAfter := rowIDsByAsset(t, pool, seeded.room.ID)
	for _, asset := range want {
		if idsAfter[asset] != idsBefore[asset] {
			t.Errorf("row identity changed for %s", asset)
		}
	}
	if _, stillThere := idsAfter[seeded.assets[2]]; stillThere {
		t.Error("the deleted row must be gone")
	}
	var assetRows int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM assets WHERE id = $1`, seeded.assets[2]).Scan(&assetRows); err != nil || assetRows != 1 {
		t.Errorf("the asset row must remain (rows=%d err=%v)", assetRows, err)
	}
}

func TestDeletePhotoSlotCompacting_AfterAReorder_StillLeavesContiguousIndices(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), domain.MaxPhotosPerRoom)

	shuffled := append([]string(nil), seeded.assets...)
	rand.New(rand.NewSource(41)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	if err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		return repo.ReorderPhotoSlots(ctx, seeded.room.ID, shuffled)
	}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		return repo.DeletePhotoSlotCompacting(ctx, seeded.room.ID, shuffled[0])
	})
	if err != nil {
		t.Fatalf("delete after reorder: %v", err)
	}

	order, _ := currentOrder(t, repo, seeded.room.ID)
	if len(order) != domain.MaxPhotosPerRoom-1 {
		t.Fatalf("expected %d slots, got %d", domain.MaxPhotosPerRoom-1, len(order))
	}
	for i := range order {
		if order[i] != shuffled[i+1] {
			t.Fatalf("slot %d holds %s, want %s", i, order[i], shuffled[i+1])
		}
	}
}

func TestDeletePhotoSlotCompacting_First_Last_AndOnly(t *testing.T) {
	cases := []struct {
		name   string
		seed   int
		pick   int
		expect func(assets []string) []string
	}{
		{"first", 4, 0, func(a []string) []string { return []string{a[1], a[2], a[3]} }},
		{"last", 4, 3, func(a []string) []string { return []string{a[0], a[1], a[2]} }},
		{"only", 1, 0, func(a []string) []string { return []string{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := testPool(t)
			repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
			ctx := context.Background()
			seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), tc.seed)

			err := pool.Run(ctx, func(ctx context.Context) error {
				if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
					return err
				}
				return repo.DeletePhotoSlotCompacting(ctx, seeded.room.ID, seeded.assets[tc.pick])
			})
			if err != nil {
				t.Fatalf("delete: %v", err)
			}
			order, _ := currentOrder(t, repo, seeded.room.ID)
			want := tc.expect(seeded.assets)
			if len(order) != len(want) {
				t.Fatalf("order = %v, want %v", order, want)
			}
			for i := range want {
				if order[i] != want[i] {
					t.Fatalf("slot %d holds %s, want %s", i, order[i], want[i])
				}
			}
		})
	}
}

func TestDeletePhotoSlotCompacting_UnknownPhotograph_IsNotInRoom(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 2)

	err := pool.Run(context.Background(), func(ctx context.Context) error {
		return repo.DeletePhotoSlotCompacting(ctx, seeded.room.ID, "00000000-0000-4000-8000-000000000000")
	})

	if !errors.Is(err, domain.ErrPhotoNotInRoom) {
		t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
	}
	if order, _ := currentOrder(t, repo, seeded.room.ID); len(order) != 2 {
		t.Error("nothing may be deleted")
	}
}

func TestDeletePhotoSlotCompacting_CannotReachAnotherRoomsPhotograph(t *testing.T) {
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

	err = pool.Run(ctx, func(ctx context.Context) error {
		return repo.DeletePhotoSlotCompacting(ctx, roomB.ID, roomA.assets[0])
	})

	if !errors.Is(err, domain.ErrPhotoNotInRoom) {
		t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
	}
	if order, _ := currentOrder(t, repo, roomA.room.ID); len(order) != 2 || order[0] != roomA.assets[0] {
		t.Error("a photograph was deleted across a Room boundary")
	}
}

func TestDeletePhotoSlotCompacting_InsideAFailedTransaction_RollsBack(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	seeded := seedRoomWithPhotos(t, repo, func(t *testing.T, acct, tag string) string { return createCommittedAsset(t, pool, acct, tag) }, createAccount(t, pool), 4)
	before, captionsBefore := currentOrder(t, repo, seeded.room.ID)
	sentinel := errors.New("later step failed")

	err := pool.Run(ctx, func(ctx context.Context) error {
		if _, err := repo.LockRoomForUpdate(ctx, seeded.room.ID); err != nil {
			return err
		}
		if err := repo.DeletePhotoSlotCompacting(ctx, seeded.room.ID, seeded.assets[1]); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}

	after, captionsAfter := currentOrder(t, repo, seeded.room.ID)
	if len(after) != len(before) {
		t.Fatalf("count changed despite rollback: %d → %d", len(before), len(after))
	}
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
