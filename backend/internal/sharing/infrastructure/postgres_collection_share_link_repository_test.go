package infrastructure_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/platform/database"
	"muse-backend/internal/sharing/domain"
	"muse-backend/internal/sharing/infrastructure"
)

func newCollectionRoomID(t *testing.T, pool *database.Pool) string {
	t.Helper()
	ctx := context.Background()
	var accountID, roomID string
	if err := pool.Pool().QueryRow(ctx, `INSERT INTO accounts (display_name) VALUES ('') RETURNING id`).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	if err := pool.Pool().QueryRow(ctx,
		`INSERT INTO collection_rooms (account_id, name, current_tier, created_at, updated_at)
		 VALUES ($1, 'Watches', 1, now(), now()) RETURNING id::text`, accountID).Scan(&roomID); err != nil {
		t.Fatalf("collection room: %v", err)
	}
	return roomID
}

func newCollectionRepo(t *testing.T) (*infrastructure.PostgresCollectionShareLinkRepository, *database.Pool, string) {
	t.Helper()
	pool := testPool(t)
	return infrastructure.NewPostgresCollectionShareLinkRepository(pool.Pool()), pool, newCollectionRoomID(t, pool)
}

func activeCollectionCount(t *testing.T, pool *database.Pool, roomID string) int {
	t.Helper()
	var n int
	if err := pool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM collection_share_links WHERE collection_room_id = $1 AND status = 'active'`, roomID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCollectionEnsureActive_CreatesOnce_ThenReturnsTheSameLink(t *testing.T) {
	repo, pool, roomID := newCollectionRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	first, err := repo.EnsureActive(ctx, roomID, code(1), now)
	if err != nil || first.Code != code(1) || first.CollectionRoomID != roomID || !first.IsActive() {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := repo.EnsureActive(ctx, roomID, code(2), now)
	if err != nil || second.Code != code(1) || second.ID != first.ID {
		t.Fatalf("second must return the existing link, not mint code(2): %+v %v", second, err)
	}
	if n := activeCollectionCount(t, pool, roomID); n != 1 {
		t.Fatalf("active: %d", n)
	}
	found, err := repo.FindActiveByRoom(ctx, roomID)
	if err != nil || found.ID != first.ID {
		t.Fatalf("find active: %+v %v", found, err)
	}
	byCode, err := repo.FindByCode(ctx, code(1))
	if err != nil || byCode.ID != first.ID {
		t.Fatalf("find by code: %+v %v", byCode, err)
	}
}

func TestCollectionEnsureActive_UnderConcurrency_ExactlyOneLink(t *testing.T) {
	repo, pool, roomID := newCollectionRepo(t)
	const racers = 8
	results := make([]domain.CollectionShareLink, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l, err := repo.EnsureActive(context.Background(), roomID, code(byte(10+i)), time.Now())
			if err != nil {
				t.Errorf("racer %d: %v", i, err)
				return
			}
			results[i] = l
		}(i)
	}
	wg.Wait()
	for i, l := range results {
		if l.ID != results[0].ID {
			t.Fatalf("racer %d got a different link: %+v vs %+v", i, l, results[0])
		}
	}
	if n := activeCollectionCount(t, pool, roomID); n != 1 {
		t.Fatalf("active: %d", n)
	}
}

func TestCollectionRotate_RevokesAndIssuesAtomically_UnderConcurrency(t *testing.T) {
	repo, pool, roomID := newCollectionRepo(t)
	ctx := context.Background()
	original, err := repo.EnsureActive(ctx, roomID, code(1), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	const racers = 8
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := repo.Rotate(context.Background(), roomID, code(byte(2+i)), time.Now()); err != nil {
				t.Errorf("rotate %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if n := activeCollectionCount(t, pool, roomID); n != 1 {
		t.Fatalf("active after concurrent rotates: %d", n)
	}
	old, err := repo.FindByCode(ctx, original.Code)
	if err != nil || old.IsActive() || old.RevokedAt == nil {
		t.Fatalf("original must be revoked with a timestamp: %+v %v", old, err)
	}
	var total int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM collection_share_links WHERE collection_room_id = $1`, roomID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != racers+1 {
		t.Fatalf("rows: %d, want %d", total, racers+1)
	}
}

func TestCollectionRevoke_ClosesWithoutReplacement_AndReportsIt(t *testing.T) {
	repo, pool, roomID := newCollectionRepo(t)
	ctx := context.Background()
	now := time.Now()

	revoked, err := repo.Revoke(ctx, roomID, now)
	if err != nil || revoked {
		t.Fatalf("revoke with nothing active must report false: %v %v", revoked, err)
	}
	if _, err := repo.EnsureActive(ctx, roomID, code(1), now); err != nil {
		t.Fatal(err)
	}
	revoked, err = repo.Revoke(ctx, roomID, now)
	if err != nil || !revoked {
		t.Fatalf("revoke: %v %v", revoked, err)
	}
	if n := activeCollectionCount(t, pool, roomID); n != 0 {
		t.Fatalf("active after revoke: %d", n)
	}
	if _, err := repo.FindActiveByRoom(ctx, roomID); !errors.Is(err, domain.ErrNoActiveCollectionLink) {
		t.Fatalf("find active after revoke: %v", err)
	}
	l, err := repo.FindByCode(ctx, code(1))
	if err != nil || l.IsActive() || l.RevokedAt == nil {
		t.Fatalf("revoked row: %+v %v", l, err)
	}
	fresh, err := repo.EnsureActive(ctx, roomID, code(2), now)
	if err != nil || fresh.Code != code(2) || fresh.ID == l.ID {
		t.Fatalf("fresh: %+v %v", fresh, err)
	}
}

func TestCollectionLinks_TwoRoomsAreIndependent(t *testing.T) {
	repo, pool, roomA := newCollectionRepo(t)
	roomB := newCollectionRoomID(t, pool)
	ctx := context.Background()
	now := time.Now()

	a, err := repo.EnsureActive(ctx, roomA, code(1), now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.EnsureActive(ctx, roomB, code(2), now)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.Code == b.Code {
		t.Fatalf("two Rooms share a link: %+v %+v", a, b)
	}
	if _, err := repo.Rotate(ctx, roomA, code(3), now); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.FindActiveByRoom(ctx, roomB); err != nil || got.ID != b.ID {
		t.Fatalf("rotating A touched B: %+v %v", got, err)
	}
	if _, err := repo.Revoke(ctx, roomB, now); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.FindActiveByRoom(ctx, roomA); err != nil || got.Code != code(3) {
		t.Fatalf("revoking B touched A: %+v %v", got, err)
	}
}

func TestCollectionLinks_SchemaConstraints(t *testing.T) {
	repo, pool, roomID := newCollectionRepo(t)
	ctx := context.Background()
	now := time.Now()
	if _, err := repo.EnsureActive(ctx, roomID, code(1), now); err != nil {
		t.Fatal(err)
	}

	_, err := pool.Pool().Exec(ctx,
		`INSERT INTO collection_share_links (collection_room_id, code, status, created_at) VALUES ($1, $2, 'active', $3)`,
		roomID, string(code(9)), now)
	if err == nil || !strings.Contains(err.Error(), "collection_share_links_one_active_per_room") {
		t.Fatalf("a second active row must violate the partial unique index, got %v", err)
	}
	if _, err := pool.Pool().Exec(ctx,
		`INSERT INTO collection_share_links (collection_room_id, code, status, created_at, revoked_at) VALUES ($1, $2, 'revoked', $3, $3)`,
		roomID, string(code(8)), now); err != nil {
		t.Fatalf("a revoked row alongside an active one must be allowed: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx,
		`INSERT INTO collection_share_links (collection_room_id, code, status, created_at, revoked_at) VALUES ($1, $2, 'revoked', $3, $3)`,
		roomID, string(code(1)), now); err == nil {
		t.Fatal("a duplicate code must be refused")
	}
	if _, err := pool.Pool().Exec(ctx,
		`INSERT INTO collection_share_links (collection_room_id, code, status, created_at) VALUES ($1, $2, 'pending', $3)`,
		roomID, string(code(7)), now); err == nil {
		t.Fatal("an unknown status must be refused")
	}
	var targets []string
	rows, err := pool.Pool().Query(ctx,
		`SELECT confrelid::regclass::text FROM pg_constraint WHERE conrelid = 'collection_share_links'::regclass AND contype = 'f'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, target)
	}
	rows.Close()
	if len(targets) != 1 || targets[0] != "collection_rooms" {
		t.Fatalf("collection_share_links references %v, want exactly [collection_rooms]", targets)
	}
	if _, err := pool.Pool().Exec(ctx, `DELETE FROM collection_rooms WHERE id = $1`, roomID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByCode(ctx, code(1)); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("a deleted Room's code must be unknown, got %v", err)
	}
}
