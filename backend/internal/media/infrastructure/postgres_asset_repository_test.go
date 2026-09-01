package infrastructure_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/media/domain"
	"muse-backend/internal/media/infrastructure"
	"muse-backend/internal/platform/database"
)

func assetPool(t *testing.T) *database.Pool {
	t.Helper()
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `TRUNCATE room_photo_slots, room_sculptures, sculptures, rooms, museums, assets, email_outbox, password_credentials, pending_signups, password_resets, auth_attempts, external_identities, accounts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func newAccount(t *testing.T, pool *database.Pool) string {
	t.Helper()
	var id string
	if err := pool.Pool().QueryRow(context.Background(), `INSERT INTO accounts (display_name) VALUES ('') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return id
}

func pendingAsset(accountID, clientUploadID string) domain.Asset {
	return domain.Asset{
		AccountID:      accountID,
		Category:       domain.CategoryRoomPhoto,
		ContentType:    domain.PhotoContentType,
		ByteSize:       4096,
		PixelWidth:     1200,
		PixelHeight:    800,
		ChecksumSHA256: strings.Repeat("a", 64),
		State:          domain.StatePending,
		ClientUploadID: clientUploadID,
		CreatedAt:      time.Now(),
	}
}

func TestCreatePending_MintsIDAndStorageKeyTogether(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	account := newAccount(t, pool)

	asset, created, err := repo.CreatePending(context.Background(), pendingAsset(account, "cuid-1"))
	if err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	if !created || asset.ID == "" {
		t.Fatalf("expected a created asset with an id, got created=%v id=%q", created, asset.ID)
	}
	if asset.StorageKey != domain.PhotoStorageKey(account, asset.ID) {
		t.Errorf("storage key %q must derive from the minted id", asset.StorageKey)
	}
	if asset.State != domain.StatePending {
		t.Errorf("state = %s, want pending", asset.State)
	}
}

func TestCreatePending_ConcurrentSameClientUploadID_ProducesExactlyOneRow(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	account := newAccount(t, pool)

	const racers = 24
	var wg sync.WaitGroup
	results := make(chan domain.Asset, racers)
	createdCount := make(chan bool, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			asset, created, err := repo.CreatePending(context.Background(), pendingAsset(account, "raced"))
			if err != nil {
				t.Errorf("CreatePending: %v", err)
				return
			}
			results <- asset
			createdCount <- created
		}()
	}
	wg.Wait()
	close(results)
	close(createdCount)

	ids := map[domain.AssetID]bool{}
	for a := range results {
		ids[a.ID] = true
	}
	creations := 0
	for c := range createdCount {
		if c {
			creations++
		}
	}
	if len(ids) != 1 {
		t.Errorf("every racer must converge on one asset, got %d distinct ids", len(ids))
	}
	if creations != 1 {
		t.Errorf("exactly one racer may report creation, got %d", creations)
	}

	var rows int
	_ = pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM assets WHERE account_id = $1`, account).Scan(&rows)
	if rows != 1 {
		t.Errorf("expected 1 row in the database, got %d", rows)
	}
}

func TestMarkCommitted_OnlyMovesPendingRows_AndReportsTheCount(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	account := newAccount(t, pool)
	ctx := context.Background()

	a, _, _ := repo.CreatePending(ctx, pendingAsset(account, "a"))
	b, _, _ := repo.CreatePending(ctx, pendingAsset(account, "b"))

	n, err := repo.MarkCommitted(ctx, []domain.AssetID{a.ID, b.ID}, time.Now())
	if err != nil || n != 2 {
		t.Fatalf("first commit: n=%d err=%v", n, err)
	}
	n, err = repo.MarkCommitted(ctx, []domain.AssetID{a.ID, b.ID}, time.Now())
	if err != nil || n != 0 {
		t.Fatalf("second commit must change nothing: n=%d err=%v", n, err)
	}

	found, _ := repo.FindOwnedByIDs(ctx, account, []domain.AssetID{a.ID})
	if len(found) != 1 || found[0].State != domain.StateCommitted || found[0].CommittedAt == nil {
		t.Errorf("committed asset must carry state and timestamp: %+v", found)
	}
}

func TestMarkDiscarded_TombstoneAllowsAFreshRetryOfTheSameClientUploadID(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	account := newAccount(t, pool)
	ctx := context.Background()

	first, _, _ := repo.CreatePending(ctx, pendingAsset(account, "again"))
	if err := repo.MarkDiscarded(ctx, first.ID, time.Now()); err != nil {
		t.Fatalf("MarkDiscarded: %v", err)
	}

	second, created, err := repo.CreatePending(ctx, pendingAsset(account, "again"))
	if err != nil {
		t.Fatalf("CreatePending after discard: %v", err)
	}
	if !created || second.ID == first.ID {
		t.Errorf("a discarded row is a tombstone, not a block; got created=%v same=%v", created, second.ID == first.ID)
	}

	if _, err := repo.MarkCommitted(ctx, []domain.AssetID{second.ID}, time.Now()); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.MarkDiscarded(ctx, second.ID, time.Now()); !errors.Is(err, domain.ErrAssetNotPending) {
		t.Errorf("committed assets must never be discarded by the sweep; got %v", err)
	}
}

func TestFindOwnedByIDs_ExcludesOtherAccountsAssets(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	ctx := context.Background()
	mine := newAccount(t, pool)
	theirs := newAccount(t, pool)

	a, _, _ := repo.CreatePending(ctx, pendingAsset(mine, "a"))
	b, _, _ := repo.CreatePending(ctx, pendingAsset(theirs, "b"))

	found, err := repo.FindOwnedByIDs(ctx, mine, []domain.AssetID{a.ID, b.ID})
	if err != nil {
		t.Fatalf("FindOwnedByIDs: %v", err)
	}
	if len(found) != 1 || found[0].ID != a.ID {
		t.Errorf("only the caller's assets may be returned, got %+v", found)
	}
}

func TestListPendingOlderThan_ReturnsOnlyStalePending(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	account := newAccount(t, pool)
	ctx := context.Background()

	stale := pendingAsset(account, "stale")
	stale.CreatedAt = time.Now().Add(-48 * time.Hour)
	staleAsset, _, _ := repo.CreatePending(ctx, stale)
	repo.CreatePending(ctx, pendingAsset(account, "fresh")) //nolint:errcheck
	committed, _, _ := repo.CreatePending(ctx, func() domain.Asset {
		a := pendingAsset(account, "old-but-committed")
		a.CreatedAt = time.Now().Add(-48 * time.Hour)
		return a
	}())
	repo.MarkCommitted(ctx, []domain.AssetID{committed.ID}, time.Now()) //nolint:errcheck

	got, err := repo.ListPendingOlderThan(ctx, time.Now().Add(-24*time.Hour), 100)
	if err != nil {
		t.Fatalf("ListPendingOlderThan: %v", err)
	}
	if len(got) != 1 || got[0].ID != staleAsset.ID {
		t.Errorf("expected only the stale pending asset, got %+v", got)
	}
}

// MARK: - Released

func TestMarkReleased_OnlyMovesCommittedRows_AndReportsTheCount(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	account := newAccount(t, pool)
	ctx := context.Background()

	committed, _, _ := repo.CreatePending(ctx, pendingAsset(account, "c"))
	pending, _, _ := repo.CreatePending(ctx, pendingAsset(account, "p"))
	if _, err := repo.MarkCommitted(ctx, []domain.AssetID{committed.ID}, time.Now()); err != nil {
		t.Fatalf("commit: %v", err)
	}

	n, err := repo.MarkReleased(ctx, []domain.AssetID{committed.ID, pending.ID}, time.Now())
	if err != nil {
		t.Fatalf("MarkReleased: %v", err)
	}
	if n != 1 {
		t.Errorf("only the committed row may move; changed %d", n)
	}

	found, _ := repo.FindOwnedByIDs(ctx, account, []domain.AssetID{committed.ID, pending.ID})
	for _, asset := range found {
		switch asset.ID {
		case committed.ID:
			if asset.State != domain.StateReleased || asset.ReleasedAt == nil || asset.CommittedAt == nil {
				t.Errorf("released asset must carry state, release and commit timestamps: %+v", asset)
			}
		case pending.ID:
			if asset.State != domain.StatePending {
				t.Errorf("pending asset must be untouched: %+v", asset)
			}
		}
	}
	if n, _ := repo.MarkReleased(ctx, []domain.AssetID{committed.ID}, time.Now()); n != 0 {
		t.Errorf("re-releasing must change nothing, changed %d", n)
	}
}

func TestMarkDiscarded_AcceptsReleased_StillRefusesCommitted(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	account := newAccount(t, pool)
	ctx := context.Background()

	released, _, _ := repo.CreatePending(ctx, pendingAsset(account, "r"))
	committed, _, _ := repo.CreatePending(ctx, pendingAsset(account, "c"))
	repo.MarkCommitted(ctx, []domain.AssetID{released.ID, committed.ID}, time.Now()) //nolint:errcheck
	if n, _ := repo.MarkReleased(ctx, []domain.AssetID{released.ID}, time.Now()); n != 1 {
		t.Fatal("release")
	}

	if err := repo.MarkDiscarded(ctx, released.ID, time.Now()); err != nil {
		t.Errorf("a released asset must be discardable: %v", err)
	}
	if err := repo.MarkDiscarded(ctx, committed.ID, time.Now()); !errors.Is(err, domain.ErrAssetNotPending) {
		t.Errorf("a committed asset must never be discarded; got %v", err)
	}
}

func TestListReleasedOlderThan_ReturnsOnlyAgedReleasedRows(t *testing.T) {
	pool := assetPool(t)
	repo := infrastructure.NewPostgresAssetRepository(pool.Pool())
	account := newAccount(t, pool)
	ctx := context.Background()

	old, _, _ := repo.CreatePending(ctx, pendingAsset(account, "old"))
	fresh, _, _ := repo.CreatePending(ctx, pendingAsset(account, "fresh"))
	committed, _, _ := repo.CreatePending(ctx, pendingAsset(account, "kept"))
	repo.MarkCommitted(ctx, []domain.AssetID{old.ID, fresh.ID, committed.ID}, time.Now()) //nolint:errcheck
	if n, _ := repo.MarkReleased(ctx, []domain.AssetID{old.ID}, time.Now().Add(-2*time.Hour)); n != 1 {
		t.Fatal("release old")
	}
	if n, _ := repo.MarkReleased(ctx, []domain.AssetID{fresh.ID}, time.Now()); n != 1 {
		t.Fatal("release fresh")
	}

	got, err := repo.ListReleasedOlderThan(ctx, time.Now().Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("ListReleasedOlderThan: %v", err)
	}
	if len(got) != 1 || got[0].ID != old.ID {
		t.Errorf("expected only the aged released asset, got %+v", got)
	}
}

func TestAssetsTable_EnforcesItsInvariants(t *testing.T) {
	pool := assetPool(t)
	account := newAccount(t, pool)
	ctx := context.Background()

	cases := []struct {
		name string
		sql  string
	}{
		{"unknown state", `INSERT INTO assets (account_id, category, storage_key, content_type, byte_size, pixel_width, pixel_height, checksum_sha256, state, client_upload_id)
			VALUES ($1, 'room_photo', 'k1', 'image/jpeg', 1, 1, 1, repeat('a', 64), 'limbo', 'c')`},
		{"non-hex checksum", `INSERT INTO assets (account_id, category, storage_key, content_type, byte_size, pixel_width, pixel_height, checksum_sha256, state, client_upload_id)
			VALUES ($1, 'room_photo', 'k2', 'image/jpeg', 1, 1, 1, repeat('Z', 64), 'pending', 'c')`},
		{"committed without timestamp", `INSERT INTO assets (account_id, category, storage_key, content_type, byte_size, pixel_width, pixel_height, checksum_sha256, state, client_upload_id)
			VALUES ($1, 'room_photo', 'k3', 'image/jpeg', 1, 1, 1, repeat('a', 64), 'committed', 'c')`},
		{"zero byte size", `INSERT INTO assets (account_id, category, storage_key, content_type, byte_size, pixel_width, pixel_height, checksum_sha256, state, client_upload_id)
			VALUES ($1, 'room_photo', 'k4', 'image/jpeg', 0, 1, 1, repeat('a', 64), 'pending', 'c')`},
		{"released without timestamp", `INSERT INTO assets (account_id, category, storage_key, content_type, byte_size, pixel_width, pixel_height, checksum_sha256, state, client_upload_id, committed_at)
			VALUES ($1, 'room_photo', 'k5', 'image/jpeg', 1, 1, 1, repeat('a', 64), 'released', 'c', now())`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Pool().Exec(ctx, tc.sql, account); err == nil {
				t.Error("expected the database to reject this row")
			}
		})
	}
}
