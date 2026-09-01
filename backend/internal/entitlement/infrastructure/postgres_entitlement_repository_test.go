package infrastructure_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/entitlement/domain"
	"muse-backend/internal/entitlement/infrastructure"
	"muse-backend/internal/platform/database"
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
	if _, err := pool.Pool().Exec(ctx, `TRUNCATE app_store_transactions, account_app_account_tokens, accounts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func newAccount(t *testing.T, pool *database.Pool) string {
	t.Helper()
	var id string
	if err := pool.Pool().QueryRow(context.Background(), `INSERT INTO accounts (display_name) VALUES ('') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func boundTransaction(accountID, original, token string) domain.AppStoreTransaction {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return domain.AppStoreTransaction{
		OriginalTransactionID: original, TransactionID: original + "-tx",
		AccountID: accountID, ProductID: "dev.muse.placeholder.collection_capacity", BundleID: "com.muse.app",
		Environment: "Sandbox", AppAccountToken: token, PurchasedAt: now, FirstVerifiedAt: now, LastVerifiedAt: now,
	}
}

func TestBind_OneTransactionOneAccount(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresEntitlementRepository(pool.Pool())
	ctx := context.Background()
	accountA, accountB := newAccount(t, pool), newAccount(t, pool)
	tokenA, _ := repo.EnsureToken(ctx, accountA)
	tokenB, _ := repo.EnsureToken(ctx, accountB)

	first, err := repo.Bind(ctx, boundTransaction(accountA, "orig-1", tokenA))
	if err != nil || first.AccountID != accountA || !first.IsActive() {
		t.Fatalf("bind: %+v %v", first, err)
	}
	again, err := repo.Bind(ctx, boundTransaction(accountA, "orig-1", tokenA))
	if err != nil || again.AccountID != accountA {
		t.Fatalf("rebind same account: %+v %v", again, err)
	}
	_, err = repo.Bind(ctx, boundTransaction(accountB, "orig-1", tokenB))
	if !errors.Is(err, domain.ErrTransactionBoundToAnotherAccount) {
		t.Fatalf("expected ErrTransactionBoundToAnotherAccount, got %v", err)
	}
	var owner string
	var rows int
	if err := pool.Pool().QueryRow(ctx, `SELECT account_id::text, (SELECT count(*) FROM app_store_transactions) FROM app_store_transactions WHERE original_transaction_id = 'orig-1'`).Scan(&owner, &rows); err != nil {
		t.Fatal(err)
	}
	if owner != accountA || rows != 1 {
		t.Fatalf("owner %s rows %d", owner, rows)
	}
	listB, _ := repo.ListForAccount(ctx, accountB)
	if len(listB) != 0 {
		t.Fatalf("B must hold nothing: %+v", listB)
	}
}

func TestBind_UnderConcurrency_ExactlyOneAccountWins(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresEntitlementRepository(pool.Pool())
	ctx := context.Background()
	accounts := make([]string, 8)
	for i := range accounts {
		accounts[i] = newAccount(t, pool)
	}
	var wg sync.WaitGroup
	winners := make([]bool, len(accounts))
	for i, account := range accounts {
		wg.Add(1)
		go func(i int, account string) {
			defer wg.Done()
			_, err := repo.Bind(ctx, boundTransaction(account, "orig-race", "00000000-0000-0000-0000-000000000000"))
			winners[i] = err == nil
		}(i, account)
	}
	wg.Wait()
	won := 0
	for _, w := range winners {
		if w {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("exactly one account may bind a transaction; %d did", won)
	}
}

func TestEnsureToken_MintsOnce_AndResolvesBack(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresEntitlementRepository(pool.Pool())
	ctx := context.Background()
	accountA, accountB := newAccount(t, pool), newAccount(t, pool)

	first, err := repo.EnsureToken(ctx, accountA)
	if err != nil || len(first) != 36 {
		t.Fatalf("token: %q %v", first, err)
	}
	second, _ := repo.EnsureToken(ctx, accountA)
	if second != first {
		t.Fatal("the same account must always get the same token")
	}
	other, _ := repo.EnsureToken(ctx, accountB)
	if other == first {
		t.Fatal("two accounts must not share a token")
	}
	if owner, err := repo.AccountForToken(ctx, first); err != nil || owner != accountA {
		t.Fatalf("resolve: %s %v", owner, err)
	}
	if _, err := repo.AccountForToken(ctx, "11111111-1111-1111-1111-111111111111"); !errors.Is(err, domain.ErrUnknownAppAccountToken) {
		t.Fatalf("unknown token: %v", err)
	}
	if _, err := repo.AccountForToken(ctx, "not-a-uuid"); !errors.Is(err, domain.ErrUnknownAppAccountToken) {
		t.Fatalf("malformed token must read as unknown, not error: %v", err)
	}
}

func TestSetRevocation_RecordsAndClears_NeverDeletes(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresEntitlementRepository(pool.Pool())
	ctx := context.Background()
	account := newAccount(t, pool)
	token, _ := repo.EnsureToken(ctx, account)
	if _, err := repo.Bind(ctx, boundTransaction(account, "orig-1", token)); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	found, err := repo.SetRevocation(ctx, "orig-1", &at, "1")
	if err != nil || !found {
		t.Fatalf("revoke: %v %v", found, err)
	}
	list, _ := repo.ListForAccount(ctx, account)
	if len(list) != 1 || list[0].IsActive() || list[0].RevocationReason != "1" {
		t.Fatalf("after revoke: %+v", list)
	}
	found, _ = repo.SetRevocation(ctx, "orig-1", nil, "")
	list, _ = repo.ListForAccount(ctx, account)
	if !found || len(list) != 1 || !list[0].IsActive() {
		t.Fatalf("after reversal: %+v", list)
	}
	if found, _ := repo.SetRevocation(ctx, "orig-never", &at, "1"); found {
		t.Fatal("an unknown transaction matches nothing")
	}
}
