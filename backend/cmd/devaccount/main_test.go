package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
	"muse-backend/internal/platform/database"
)

func newTestPool(t *testing.T) *database.Pool {
	t.Helper()
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping devaccount tests")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.ApplyMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx,
		`TRUNCATE password_credentials, pending_signups, password_resets, auth_attempts, external_identities, accounts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func withDevEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", os.Getenv("TEST_DATABASE_URL"))
}

// MARK: - The environment gate

func TestDevAccount_RefusesOutsideExplicitDevelopment(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	for name, env := range map[string]string{
		"production": "production",
		"staging":    "staging",
		"unset":      "",
		"typo":       "Development",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			t.Setenv("DATABASE_URL", os.Getenv("TEST_DATABASE_URL"))
			err := run(context.Background(), defaultEmail, defaultPassword, defaultAvatar, false)
			if err == nil {
				t.Fatalf("APP_ENV=%q must be refused", env)
			}
		})
	}
}

func TestDevAccount_RequiresADatabaseURL(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "")
	if err := run(context.Background(), defaultEmail, defaultPassword, defaultAvatar, false); err == nil {
		t.Fatal("a missing DATABASE_URL must be refused")
	}
}

// MARK: - Input validation, against the real domain rules

func TestDevAccount_RefusesInputTheRealSignupWouldRefuse(t *testing.T) {
	pool := newTestPool(t)
	withDevEnvironment(t)

	cases := map[string]struct{ email, password, avatar string }{
		"malformed email": {"not-an-email", defaultPassword, defaultAvatar},
		"short password":  {defaultEmail, "short", defaultAvatar},
		"unknown avatar":  {defaultEmail, defaultPassword, "avatar_99"},
		"empty avatar":    {defaultEmail, defaultPassword, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := run(context.Background(), c.email, c.password, c.avatar, false); err == nil {
				t.Fatalf("%s must be refused", name)
			}
		})
	}

	var accounts int
	if err := pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM accounts`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 0 {
		t.Errorf("a refused run must write nothing, found %d accounts", accounts)
	}
}

// MARK: - What it creates

func TestDevAccount_CreatesAnActiveAccountWithACredentialAndAnAvatar(t *testing.T) {
	pool := newTestPool(t)
	withDevEnvironment(t)
	ctx := context.Background()

	if err := run(ctx, defaultEmail, defaultPassword, defaultAvatar, false); err != nil {
		t.Fatalf("create: %v", err)
	}

	credentials := infrastructure.NewPostgresPasswordRepository(pool.Pool())
	credential, err := credentials.FindByEmail(ctx, domain.EmailAddress(defaultEmail))
	if err != nil {
		t.Fatalf("credential: %v", err)
	}

	accounts := application.NewAccountService(infrastructure.NewPostgresAccountRepository(pool.Pool()))
	account, err := accounts.FindByID(ctx, credential.AccountID)
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if account.IsDeleted() {
		t.Error("the account must be active")
	}
	if account.AvatarID != domain.AvatarID(defaultAvatar) {
		t.Errorf("avatar = %q, want %q — Avatar onboarding would block sign-in without it", account.AvatarID, defaultAvatar)
	}

	hasher := infrastructure.NewDefaultArgon2idHasher()
	ok, needsRehash, err := hasher.Verify(defaultPassword, credential.Hash)
	if err != nil || !ok {
		t.Fatalf("the production hasher must verify the stored hash: ok=%v err=%v", ok, err)
	}
	if needsRehash {
		t.Error("the hash must already be at current parameters")
	}
	if credential.Hash == defaultPassword {
		t.Fatal("the password must never be stored in a reversible form")
	}

	var museums int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM museums`).Scan(&museums); err != nil {
		t.Fatal(err)
	}
	if museums != 0 {
		t.Errorf("no Museum may be created, found %d", museums)
	}

	var pending int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM pending_signups`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Errorf("no pending signup may remain, found %d", pending)
	}
}

// MARK: - The assertion that matters: the real login path accepts it

func TestDevAccount_SignsInThroughTheRealLoginPath(t *testing.T) {
	pool := newTestPool(t)
	withDevEnvironment(t)
	ctx := context.Background()

	if err := run(ctx, defaultEmail, defaultPassword, defaultAvatar, false); err != nil {
		t.Fatalf("create: %v", err)
	}

	passwords, tokens := buildRealAuthStack(t, pool)

	result, err := passwords.LogIn(ctx, defaultEmail, defaultPassword, "test-source")
	if err != nil {
		t.Fatalf("the real login path must accept the seeded account: %v", err)
	}
	if result.IsNewAccount {
		t.Error("a seeded account is not a new account — the app must route to the hub, not Account Creation")
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatal("login must issue a real session")
	}

	claims, err := tokens.Verify(result.AccessToken)
	if err != nil {
		t.Fatalf("the issued access token must verify: %v", err)
	}
	credential, err := infrastructure.NewPostgresPasswordRepository(pool.Pool()).
		FindByEmail(ctx, domain.EmailAddress(defaultEmail))
	if err != nil {
		t.Fatal(err)
	}
	if claims.AccountID != credential.AccountID {
		t.Errorf("token account %q != credential account %q", claims.AccountID, credential.AccountID)
	}

	if _, err := passwords.LogIn(ctx, defaultEmail, "WrongPassword123!", "test-source"); err == nil {
		t.Error("a wrong password must be refused")
	}
}

// MARK: - Never silently mutate

func TestDevAccount_SecondRun_ReportsAndChangesNothing(t *testing.T) {
	pool := newTestPool(t)
	withDevEnvironment(t)
	ctx := context.Background()

	if err := run(ctx, defaultEmail, defaultPassword, defaultAvatar, false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	credentials := infrastructure.NewPostgresPasswordRepository(pool.Pool())
	before, err := credentials.FindByEmail(ctx, domain.EmailAddress(defaultEmail))
	if err != nil {
		t.Fatal(err)
	}

	err = run(ctx, defaultEmail, "ADifferentPassword123!", "avatar_5", false)
	if err == nil {
		t.Fatal("a second run must report the existing account rather than succeed silently")
	}
	if err := run(ctx, defaultEmail, defaultPassword, defaultAvatar, true); err != nil {
		t.Fatalf("-show-existing must not error on an existing account: %v", err)
	}

	after, err := credentials.FindByEmail(ctx, domain.EmailAddress(defaultEmail))
	if err != nil {
		t.Fatal(err)
	}
	if after.Hash != before.Hash {
		t.Error("the stored password hash must not be overwritten")
	}
	if after.AccountID != before.AccountID {
		t.Error("the account must not be replaced")
	}

	accounts := application.NewAccountService(infrastructure.NewPostgresAccountRepository(pool.Pool()))
	account, err := accounts.FindByID(ctx, before.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AvatarID != domain.AvatarID(defaultAvatar) {
		t.Errorf("the avatar must not be changed by a refused run: %q", account.AvatarID)
	}

	var count int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("exactly one account must exist, found %d", count)
	}

	passwords, _ := buildRealAuthStack(t, pool)
	if _, err := passwords.LogIn(ctx, defaultEmail, defaultPassword, "s"); err != nil {
		t.Errorf("the original password must still sign in: %v", err)
	}
}

func TestDevAccount_ShowExisting_OnAnAbsentAccount_IsNotAnError(t *testing.T) {
	newTestPool(t)
	withDevEnvironment(t)
	if err := run(context.Background(), defaultEmail, defaultPassword, defaultAvatar, true); err != nil {
		t.Fatalf("-show-existing on an absent account must not error: %v", err)
	}
}

// MARK: - Helpers

func buildRealAuthStack(t *testing.T, pool *database.Pool) (*application.PasswordService, *infrastructure.AccessTokenSigner) {
	t.Helper()
	db := pool.Pool()
	signer := infrastructure.NewAccessTokenSigner([]byte("devaccount-test-signing-key-long-enough"), "muse-backend", time.Hour)
	sessions := infrastructure.NewInMemorySessionStore()
	accountRepo := infrastructure.NewPostgresAccountRepository(db)
	login := application.NewLoginService(
		nil, application.NewAccountService(accountRepo), sessions, signer,
		infrastructure.OpaqueRefreshTokenGenerator{}, 180*24*time.Hour, 30*24*time.Hour,
	)
	passwords := application.NewPasswordService(
		infrastructure.NewPostgresPasswordRepository(db),
		infrastructure.NewPostgresPendingSignupRepository(db),
		infrastructure.NewPostgresPasswordResetRepository(db),
		accountRepo,
		sessions,
		login,
		infrastructure.NewDefaultArgon2idHasher(),
		infrastructure.OpaqueRefreshTokenGenerator{},
		discardingEmailSender{},
		infrastructure.NewPostgresAttemptLimiter(db, infrastructure.DefaultAttemptPolicy),
		infrastructure.NewPostgresEmailOutbox(db),
	)
	return passwords, signer
}

type discardingEmailSender struct{}

func (discardingEmailSender) SendEmailVerification(context.Context, domain.EmailAddress, string) error {
	return errors.New("devaccount tests send no email")
}
func (discardingEmailSender) SendPasswordReset(context.Context, domain.EmailAddress, string) error {
	return errors.New("devaccount tests send no email")
}
func (discardingEmailSender) SendSignupOnExistingAccount(context.Context, domain.EmailAddress) error {
	return errors.New("devaccount tests send no email")
}
