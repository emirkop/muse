package infrastructure_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	catalinfra "muse-backend/internal/catalog/infrastructure"
	museumdomain "muse-backend/internal/museum/domain"
	museuminfra "muse-backend/internal/museum/infrastructure"
	"muse-backend/internal/platform/database"
	"muse-backend/internal/sharing/domain"
	"muse-backend/internal/sharing/infrastructure"
)

func testPool(t *testing.T) *database.Pool {
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
	if err := catalinfra.NewPostgresCatalogRepository(pool.Pool()).EnsureSeeded(ctx); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `TRUNCATE collection_share_links, collection_items, collection_rooms, share_links, room_photo_slots, room_sculptures, rooms, museums, accounts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func newMuseumID(t *testing.T, pool *database.Pool) string {
	t.Helper()
	ctx := context.Background()
	var accountID string
	if err := pool.Pool().QueryRow(ctx, `INSERT INTO accounts (display_name) VALUES ('') RETURNING id`).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	museum, err := museuminfra.NewPostgresMuseumRepository(pool.Pool()).CreateMuseum(ctx, museumdomain.Museum{
		AccountID: accountID, StyleID: "style_modern", Privacy: museumdomain.PrivacyPrivate,
	})
	if err != nil {
		t.Fatalf("museum: %v", err)
	}
	return string(museum.ID)
}

func newRepo(t *testing.T) (*infrastructure.PostgresShareLinkRepository, *database.Pool, string) {
	t.Helper()
	pool := testPool(t)
	return infrastructure.NewPostgresShareLinkRepository(pool.Pool()), pool, newMuseumID(t, pool)
}

func code(seed byte) domain.Code {
	b := make([]byte, 22)
	for i := range b {
		b[i] = 'a' + (seed+byte(i))%26
	}
	return domain.Code(b)
}

func activeCount(t *testing.T, pool *database.Pool, museumID string) int {
	t.Helper()
	var n int
	if err := pool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM share_links WHERE museum_id = $1 AND status = 'active'`, museumID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnsureActive_CreatesOnce_ThenReturnsTheSameLink(t *testing.T) {
	repo, pool, museumID := newRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	first, err := repo.EnsureActive(ctx, museumID, code(1), now)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := repo.EnsureActive(ctx, museumID, code(2), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if second.ID != first.ID || second.Code != code(1) {
		t.Fatalf("the second call must return the first link unchanged: %+v vs %+v", first, second)
	}
	if !first.IsActive() || first.RevokedAt != nil || !first.CreatedAt.Equal(now) {
		t.Fatalf("unexpected first link %+v", first)
	}
	if activeCount(t, pool, museumID) != 1 {
		t.Fatal("exactly one active link")
	}
}

func TestFindActiveByMuseum_NoneIsNotAnError(t *testing.T) {
	repo, _, museumID := newRepo(t)

	_, err := repo.FindActiveByMuseum(context.Background(), museumID)

	if !errors.Is(err, domain.ErrNoActiveLink) {
		t.Fatalf("got %v, want ErrNoActiveLink", err)
	}
}

func TestFindByCode_ReturnsRevokedLinksToo_AndNotAvailableForUnknown(t *testing.T) {
	repo, _, museumID := newRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	old, _ := repo.EnsureActive(ctx, museumID, code(1), now)
	if _, err := repo.Rotate(ctx, museumID, code(2), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	revoked, err := repo.FindByCode(ctx, old.Code)
	if err != nil {
		t.Fatalf("a revoked link is still a row: %v", err)
	}
	if revoked.IsActive() || revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("expected revoked at rotation instant, got %+v", revoked)
	}
	if _, err := repo.FindByCode(ctx, code(9)); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("unknown code: got %v, want ErrLinkNotAvailable", err)
	}
}

func TestRotate_RevokesTheActive_AndIssuesTheNew_Atomically(t *testing.T) {
	repo, pool, museumID := newRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	old, _ := repo.EnsureActive(ctx, museumID, code(1), now)

	fresh, err := repo.Rotate(ctx, museumID, code(2), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if fresh.Code != code(2) || !fresh.IsActive() {
		t.Fatalf("unexpected new link %+v", fresh)
	}
	current, _ := repo.FindActiveByMuseum(ctx, museumID)
	if current.ID != fresh.ID {
		t.Fatal("the new link is the only active one")
	}
	previous, _ := repo.FindByCode(ctx, old.Code)
	if previous.IsActive() {
		t.Fatal("the old link must be revoked")
	}
	if activeCount(t, pool, museumID) != 1 {
		t.Fatal("exactly one active link after rotation")
	}
}

func TestRotate_WithNoPriorLink_IssuesOne(t *testing.T) {
	repo, pool, museumID := newRepo(t)

	fresh, err := repo.Rotate(context.Background(), museumID, code(3), time.Now())

	if err != nil || !fresh.IsActive() {
		t.Fatalf("got %+v, %v", fresh, err)
	}
	if activeCount(t, pool, museumID) != 1 {
		t.Fatal("exactly one active link")
	}
}

func TestSchema_RejectsASecondActiveLink_AndAnInconsistentRevocation(t *testing.T) {
	repo, pool, museumID := newRepo(t)
	ctx := context.Background()
	if _, err := repo.EnsureActive(ctx, museumID, code(1), time.Now()); err != nil {
		t.Fatal(err)
	}

	_, err := pool.Pool().Exec(ctx,
		`INSERT INTO share_links (museum_id, code, status, created_at) VALUES ($1, $2, 'active', now())`,
		museumID, string(code(2)))
	if err == nil {
		t.Fatal("the partial unique index must reject a second active link")
	}

	_, err = pool.Pool().Exec(ctx,
		`INSERT INTO share_links (museum_id, code, status, created_at, revoked_at) VALUES ($1, $2, 'active', now(), now())`,
		museumID, string(code(3)))
	if err == nil {
		t.Fatal("an active link with a revoked_at must be rejected")
	}
	_, err = pool.Pool().Exec(ctx,
		`INSERT INTO share_links (museum_id, code, status, created_at) VALUES ($1, $2, 'revoked', now())`,
		museumID, string(code(4)))
	if err == nil {
		t.Fatal("a revoked link without revoked_at must be rejected")
	}
}

func TestEnsureActive_ConcurrentCallers_AllGetTheSameLink(t *testing.T) {
	repo, pool, museumID := newRepo(t)
	ctx := context.Background()
	const callers = 8

	results := make([]domain.ShareLink, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = repo.EnsureActive(ctx, museumID, code(byte(i)), time.Now())
		}(i)
	}
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if results[i].ID != results[0].ID {
			t.Fatalf("caller %d got a different link: %+v vs %+v", i, results[i], results[0])
		}
	}
	var total int
	_ = pool.Pool().QueryRow(ctx, `SELECT count(*) FROM share_links WHERE museum_id = $1`, museumID).Scan(&total)
	if total != 1 {
		t.Fatalf("exactly one row, got %d", total)
	}
}

func TestRotate_ConcurrentCallers_LeaveExactlyOneActive(t *testing.T) {
	repo, pool, museumID := newRepo(t)
	ctx := context.Background()
	if _, err := repo.EnsureActive(ctx, museumID, code(100), time.Now()); err != nil {
		t.Fatal(err)
	}
	const callers = 8

	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.Rotate(ctx, museumID, code(byte(i)), time.Now())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("rotation %d failed: %v", i, err)
		}
	}
	if n := activeCount(t, pool, museumID); n != 1 {
		t.Fatalf("exactly one active link after concurrent rotations, got %d", n)
	}
	var total, revoked int
	_ = pool.Pool().QueryRow(ctx, `SELECT count(*), count(revoked_at) FROM share_links WHERE museum_id = $1`, museumID).Scan(&total, &revoked)
	if total != callers+1 || revoked != callers {
		t.Fatalf("expected %d rows with %d revoked, got %d / %d", callers+1, callers, total, revoked)
	}
}

func TestLinks_AreScopedToTheirMuseum(t *testing.T) {
	repo, pool, museumA := newRepo(t)
	museumB := newMuseumID(t, pool)
	ctx := context.Background()

	a, _ := repo.EnsureActive(ctx, museumA, code(1), time.Now())
	b, _ := repo.EnsureActive(ctx, museumB, code(2), time.Now())
	if _, err := repo.Rotate(ctx, museumB, code(3), time.Now()); err != nil {
		t.Fatal(err)
	}

	stillA, _ := repo.FindByCode(ctx, a.Code)
	if !stillA.IsActive() {
		t.Fatal("rotating B must not touch A")
	}
	oldB, _ := repo.FindByCode(ctx, b.Code)
	if oldB.IsActive() {
		t.Fatal("B's old link must be revoked")
	}
}

func TestEnsureAndRotate_ConcurrentAcrossManyMuseums_NeverCross(t *testing.T) {
	repo, pool, _ := newRepo(t)
	ctx := context.Background()
	const museums = 12

	ids := make([]string, museums)
	for i := range ids {
		ids[i] = newMuseumID(t, pool)
	}

	gen := infrastructure.RandomCodeGenerator{}
	mint := func() domain.Code {
		c, err := gen.NewCode()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	rotated := make([]domain.Code, museums)
	for i := range rotated {
		rotated[i] = mint()
	}

	links := make([]domain.ShareLink, museums)
	errs := make([]error, museums)
	var wg sync.WaitGroup
	for i := 0; i < museums; i++ {
		first, again := mint(), mint()
		wg.Add(1)
		go func(i int, first, again domain.Code) {
			defer wg.Done()
			if _, err := repo.EnsureActive(ctx, ids[i], first, time.Now()); err != nil {
				errs[i] = err
				return
			}
			if _, err := repo.Rotate(ctx, ids[i], rotated[i], time.Now()); err != nil {
				errs[i] = err
				return
			}
			links[i], errs[i] = repo.EnsureActive(ctx, ids[i], again, time.Now())
		}(i, first, again)
	}
	wg.Wait()

	seen := map[domain.Code]bool{}
	for i := range ids {
		if errs[i] != nil {
			t.Fatalf("museum %d: %v", i, errs[i])
		}
		if links[i].MuseumID != ids[i] {
			t.Fatalf("museum %d's active link belongs to %s", i, links[i].MuseumID)
		}
		if links[i].Code != rotated[i] {
			t.Fatalf("museum %d: the rotated code must be the active one, got %s", i, links[i].Code)
		}
		if seen[links[i].Code] {
			t.Fatalf("code %s issued to two Museums", links[i].Code)
		}
		seen[links[i].Code] = true
		if activeCount(t, pool, ids[i]) != 1 {
			t.Fatalf("museum %d: exactly one active link", i)
		}
	}
}
