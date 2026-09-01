package infrastructure_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
)

func newPasswordRepos(t *testing.T) (
	*infrastructure.PostgresPasswordRepository,
	*infrastructure.PostgresPendingSignupRepository,
	*infrastructure.PostgresPasswordResetRepository,
) {
	t.Helper()
	pool := testPool(t)
	return infrastructure.NewPostgresPasswordRepository(pool.Pool()),
		infrastructure.NewPostgresPendingSignupRepository(pool.Pool()),
		infrastructure.NewPostgresPasswordResetRepository(pool.Pool())
}

func TestPostgresPasswordRepository_CreateAndFind(t *testing.T) {
	credentials, _, _ := newPasswordRepos(t)
	ctx := context.Background()

	account, err := credentials.CreateAccountWithCredential(ctx,
		domain.Account{},
		domain.PasswordCredential{Email: "someone@example.com", Hash: "$argon2id$stub"},
	)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if account.ID == "" {
		t.Fatal("the created account must have an id")
	}

	byEmail, err := credentials.FindByEmail(ctx, "someone@example.com")
	if err != nil {
		t.Fatalf("find by email: %v", err)
	}
	if byEmail.AccountID != account.ID {
		t.Fatalf("credential points at %q, want %q", byEmail.AccountID, account.ID)
	}
	if byEmail.Hash != "$argon2id$stub" {
		t.Fatalf("hash round-tripped as %q", byEmail.Hash)
	}

	byAccount, err := credentials.FindByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("find by account: %v", err)
	}
	if byAccount.Email != "someone@example.com" {
		t.Fatalf("email round-tripped as %q", byAccount.Email)
	}
}

func TestPostgresPasswordRepository_UnknownEmailIsNotFound(t *testing.T) {
	credentials, _, _ := newPasswordRepos(t)

	_, err := credentials.FindByEmail(context.Background(), "nobody@example.com")

	if !errors.Is(err, domain.ErrCredentialNotFound) {
		t.Fatalf("expected ErrCredentialNotFound, got %v", err)
	}
}

func TestPostgresPasswordRepository_DuplicateEmailIsRefusedByTheDatabase(t *testing.T) {
	credentials, _, _ := newPasswordRepos(t)
	ctx := context.Background()
	if _, err := credentials.CreateAccountWithCredential(ctx, domain.Account{},
		domain.PasswordCredential{Email: "taken@example.com", Hash: "$argon2id$a"}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := credentials.CreateAccountWithCredential(ctx, domain.Account{},
		domain.PasswordCredential{Email: "taken@example.com", Hash: "$argon2id$b"})

	if !errors.Is(err, domain.ErrEmailAlreadyRegistered) {
		t.Fatalf("expected ErrEmailAlreadyRegistered, got %v", err)
	}
}

func TestPostgresPasswordRepository_FailedCredentialRollsBackTheAccount(t *testing.T) {
	credentials, _, _ := newPasswordRepos(t)
	pool := testPool(t)
	ctx := context.Background()
	if _, err := credentials.CreateAccountWithCredential(ctx, domain.Account{},
		domain.PasswordCredential{Email: "taken@example.com", Hash: "$argon2id$a"}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	var before int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&before); err != nil {
		t.Fatalf("count accounts: %v", err)
	}

	if _, err := credentials.CreateAccountWithCredential(ctx, domain.Account{},
		domain.PasswordCredential{Email: "taken@example.com", Hash: "$argon2id$b"}); err == nil {
		t.Fatal("expected the duplicate to be refused")
	}

	var after int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&after); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if after != before {
		t.Fatalf("a refused credential left an orphan account behind: %d → %d", before, after)
	}
}

func TestPostgresPasswordRepository_ConcurrentCreatesYieldOneAccount(t *testing.T) {
	credentials, _, _ := newPasswordRepos(t)
	pool := testPool(t)
	ctx := context.Background()

	const racers = 5
	var wg sync.WaitGroup
	results := make([]error, racers)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(index int) {
			defer wg.Done()
			_, err := credentials.CreateAccountWithCredential(ctx, domain.Account{},
				domain.PasswordCredential{Email: "race@example.com", Hash: fmt.Sprintf("$argon2id$%d", index)})
			results[index] = err
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
			continue
		}
		if !errors.Is(err, domain.ErrEmailAlreadyRegistered) {
			t.Fatalf("unexpected error from a racer: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("exactly one racer must win, %d did", succeeded)
	}

	var credentialCount int
	if err := pool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM password_credentials WHERE email = 'race@example.com'`).Scan(&credentialCount); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if credentialCount != 1 {
		t.Fatalf("expected exactly one credential, found %d", credentialCount)
	}
}

func TestPostgresPasswordRepository_UpdateHash(t *testing.T) {
	credentials, _, _ := newPasswordRepos(t)
	ctx := context.Background()
	account, err := credentials.CreateAccountWithCredential(ctx, domain.Account{},
		domain.PasswordCredential{Email: "someone@example.com", Hash: "$argon2id$old"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := credentials.UpdateHash(ctx, account.ID, "$argon2id$new"); err != nil {
		t.Fatalf("update: %v", err)
	}

	updated, err := credentials.FindByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if updated.Hash != "$argon2id$new" {
		t.Fatalf("hash is %q, want the updated value", updated.Hash)
	}
	if err := credentials.UpdateHash(ctx, "00000000-0000-0000-0000-000000000000", "$argon2id$x"); !errors.Is(err, domain.ErrCredentialNotFound) {
		t.Fatalf("updating an unknown account must report not-found, got %v", err)
	}
}

func TestPostgresPendingSignup_UpsertReplacesTheOutstandingAttempt(t *testing.T) {
	_, pending, _ := newPasswordRepos(t)
	ctx := context.Background()
	now := time.Now()

	first := domain.PendingSignup{
		ID: "signup-1", Email: "new@example.com", PasswordHash: "$argon2id$a",
		TokenDigest: "digest-one", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := pending.Upsert(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second := domain.PendingSignup{
		ID: "signup-1", Email: "new@example.com", PasswordHash: "$argon2id$b",
		TokenDigest: "digest-two", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := pending.Upsert(ctx, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if _, err := pending.FindByTokenDigest(ctx, "digest-one"); !errors.Is(err, domain.ErrVerificationTokenInvalid) {
		t.Fatalf("the replaced token digest must be gone, got %v", err)
	}
	found, err := pending.FindByTokenDigest(ctx, "digest-two")
	if err != nil {
		t.Fatalf("the current token must resolve: %v", err)
	}
	if found.PasswordHash != "$argon2id$b" {
		t.Fatal("the replacement's password hash must win")
	}
}

func TestPostgresPendingSignup_UpsertClearsAPreviousConsumption(t *testing.T) {
	_, pending, _ := newPasswordRepos(t)
	ctx := context.Background()
	now := time.Now()
	if err := pending.Upsert(ctx, domain.PendingSignup{
		ID: "signup-1", Email: "new@example.com", PasswordHash: "$argon2id$a",
		TokenDigest: "digest-one", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := pending.Consume(ctx, "signup-1"); err != nil {
		t.Fatalf("consume: %v", err)
	}

	if err := pending.Upsert(ctx, domain.PendingSignup{
		ID: "signup-1", Email: "new@example.com", PasswordHash: "$argon2id$b",
		TokenDigest: "digest-two", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	found, err := pending.FindByTokenDigest(ctx, "digest-two")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.ConsumedAt != nil {
		t.Fatal("a fresh attempt must not inherit the previous consumption")
	}
}

func TestPostgresPendingSignup_ConcurrentConsumeYieldsOneWinner(t *testing.T) {
	_, pending, _ := newPasswordRepos(t)
	ctx := context.Background()
	now := time.Now()
	if err := pending.Upsert(ctx, domain.PendingSignup{
		ID: "signup-1", Email: "new@example.com", PasswordHash: "$argon2id$a",
		TokenDigest: "digest-one", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	const racers = 5
	var wg sync.WaitGroup
	results := make([]error, racers)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(index int) {
			defer wg.Done()
			results[index] = pending.Consume(ctx, "signup-1")
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, domain.ErrVerificationTokenInvalid) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("exactly one consumption must succeed, %d did", winners)
	}
}

func TestPostgresPasswordReset_CreateFindConsume(t *testing.T) {
	credentials, _, resets := newPasswordRepos(t)
	ctx := context.Background()
	account, err := credentials.CreateAccountWithCredential(ctx, domain.Account{},
		domain.PasswordCredential{Email: "someone@example.com", Hash: "$argon2id$a"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	now := time.Now()

	if err := resets.Create(ctx, domain.PasswordReset{
		ID: "reset-1", AccountID: account.ID, TokenDigest: "reset-digest",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("create reset: %v", err)
	}

	found, err := resets.FindByTokenDigest(ctx, "reset-digest")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.AccountID != account.ID {
		t.Fatalf("reset points at %q, want %q", found.AccountID, account.ID)
	}

	if err := resets.Consume(ctx, "reset-1"); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if err := resets.Consume(ctx, "reset-1"); !errors.Is(err, domain.ErrResetTokenInvalid) {
		t.Fatalf("a reset token must be single-use, got %v", err)
	}
}

func TestPostgresPasswordReset_InvalidateAllForAccount(t *testing.T) {
	credentials, _, resets := newPasswordRepos(t)
	ctx := context.Background()
	account, err := credentials.CreateAccountWithCredential(ctx, domain.Account{},
		domain.PasswordCredential{Email: "someone@example.com", Hash: "$argon2id$a"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	now := time.Now()
	for i, digest := range []string{"d1", "d2", "d3"} {
		if err := resets.Create(ctx, domain.PasswordReset{
			ID: fmt.Sprintf("reset-%d", i), AccountID: account.ID, TokenDigest: digest,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}); err != nil {
			t.Fatalf("create reset %d: %v", i, err)
		}
	}

	if err := resets.InvalidateAllForAccount(ctx, account.ID); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	for _, digest := range []string{"d1", "d2", "d3"} {
		found, err := resets.FindByTokenDigest(ctx, digest)
		if err != nil {
			t.Fatalf("find %s: %v", digest, err)
		}
		if found.ConsumedAt == nil {
			t.Fatalf("token %s must be consumed after invalidate-all", digest)
		}
	}
}

func TestPostgresAttemptLimiter_LocksAfterTheConfiguredFailures(t *testing.T) {
	pool := testPool(t)
	policy := infrastructure.AttemptPolicy{MaxFailures: 3, Window: time.Minute, Lockout: time.Minute}
	limiter := infrastructure.NewPostgresAttemptLimiter(pool.Pool(), policy)
	ctx := context.Background()

	for i := 0; i < policy.MaxFailures-1; i++ {
		if err := limiter.RecordFailure(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
			t.Fatalf("record failure %d: %v", i, err)
		}
		if err := limiter.Check(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
			t.Fatalf("must not be locked after %d failures: %v", i+1, err)
		}
	}

	if err := limiter.RecordFailure(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("final failure: %v", err)
	}

	if err := limiter.Check(ctx, application.AttemptScopeLogin, "key-a"); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("expected a lockout after %d failures, got %v", policy.MaxFailures, err)
	}
}

func TestPostgresAttemptLimiter_ScopesAreIndependent(t *testing.T) {
	pool := testPool(t)
	policy := infrastructure.AttemptPolicy{MaxFailures: 2, Window: time.Minute, Lockout: time.Minute}
	limiter := infrastructure.NewPostgresAttemptLimiter(pool.Pool(), policy)
	ctx := context.Background()

	for i := 0; i < policy.MaxFailures; i++ {
		if err := limiter.RecordFailure(ctx, application.AttemptScopePasswordReset, "key-a"); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	if err := limiter.Check(ctx, application.AttemptScopePasswordReset, "key-a"); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatal("the reset scope must be locked")
	}
	if err := limiter.Check(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("a flood of reset requests must not lock someone out of logging in: %v", err)
	}
}

func TestPostgresAttemptLimiter_ResetClearsTheLock(t *testing.T) {
	pool := testPool(t)
	policy := infrastructure.AttemptPolicy{MaxFailures: 1, Window: time.Minute, Lockout: time.Minute}
	limiter := infrastructure.NewPostgresAttemptLimiter(pool.Pool(), policy)
	ctx := context.Background()
	if err := limiter.RecordFailure(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := limiter.Check(ctx, application.AttemptScopeLogin, "key-a"); err == nil {
		t.Fatal("expected a lock")
	}

	if err := limiter.Reset(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if err := limiter.Check(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("a successful login must clear the lock: %v", err)
	}
}

func TestPostgresAttemptLimiter_StaleWindowRestartsTheCount(t *testing.T) {
	pool := testPool(t)
	policy := infrastructure.AttemptPolicy{MaxFailures: 2, Window: time.Minute, Lockout: time.Minute}
	limiter := infrastructure.NewPostgresAttemptLimiter(pool.Pool(), policy)
	ctx := context.Background()

	base := time.Now()
	limiter.SetClock(func() time.Time { return base })
	if err := limiter.RecordFailure(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("first failure: %v", err)
	}

	limiter.SetClock(func() time.Time { return base.Add(time.Hour) })
	if err := limiter.RecordFailure(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("later failure: %v", err)
	}

	if err := limiter.Check(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("two failures an hour apart must not lock the key: %v", err)
	}
}

func TestPostgresAttemptLimiter_LockoutExpires(t *testing.T) {
	pool := testPool(t)
	policy := infrastructure.AttemptPolicy{MaxFailures: 1, Window: time.Minute, Lockout: 5 * time.Minute}
	limiter := infrastructure.NewPostgresAttemptLimiter(pool.Pool(), policy)
	ctx := context.Background()
	base := time.Now()
	limiter.SetClock(func() time.Time { return base })
	if err := limiter.RecordFailure(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := limiter.Check(ctx, application.AttemptScopeLogin, "key-a"); err == nil {
		t.Fatal("expected a lock")
	}

	limiter.SetClock(func() time.Time { return base.Add(policy.Lockout + time.Minute) })

	if err := limiter.Check(ctx, application.AttemptScopeLogin, "key-a"); err != nil {
		t.Fatalf("the lockout must expire on its own: %v", err)
	}
}

func TestPostgresAttemptLimiter_StoresOnlyWhatItIsGiven(t *testing.T) {
	pool := testPool(t)
	limiter := infrastructure.NewPostgresAttemptLimiter(pool.Pool(), infrastructure.DefaultAttemptPolicy)
	ctx := context.Background()
	keyDigest := domain.DigestOpaqueToken("someone@example.com")

	if err := limiter.RecordFailure(ctx, application.AttemptScopeLogin, keyDigest); err != nil {
		t.Fatalf("record: %v", err)
	}

	var stored string
	if err := pool.Pool().QueryRow(ctx,
		`SELECT key_digest FROM auth_attempts WHERE scope = $1`, string(application.AttemptScopeLogin),
	).Scan(&stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != keyDigest {
		t.Fatalf("stored key is %q, want the digest", stored)
	}
	if stored == "someone@example.com" {
		t.Fatal("the throttle table must never contain a plaintext address")
	}
}
