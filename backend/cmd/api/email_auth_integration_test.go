package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	identityapp "muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	identityinfra "muse-backend/internal/identity/infrastructure"
	identityiface "muse-backend/internal/identity/interfaces"
	"muse-backend/internal/platform/database"
	platformhttp "muse-backend/internal/platform/http"
)

type emailAuthStack struct {
	server    *httptest.Server
	pool      *database.Pool
	email     *recordingEmailSender
	limiter   *identityinfra.PostgresAttemptLimiter
	sessions  *identityinfra.InMemorySessionStore
	passwords *identityapp.PasswordService
}

func (s *emailAuthStack) drain(t *testing.T) identityapp.DrainReport {
	t.Helper()
	report, err := s.passwords.DrainEmailOutbox(context.Background())
	if err != nil {
		t.Fatalf("drain outbox: %v", err)
	}
	return report
}

type recordingEmailSender struct {
	mu              sync.Mutex
	verifications   []string
	resets          []string
	existingNotices int
	failNextReset   bool
}

func (s *recordingEmailSender) SendEmailVerification(_ context.Context, _ domain.EmailAddress, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.verifications = append(s.verifications, token)
	return nil
}

func (s *recordingEmailSender) SendPasswordReset(_ context.Context, _ domain.EmailAddress, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNextReset {
		s.failNextReset = false
		return errors.New("provider unavailable")
	}
	s.resets = append(s.resets, token)
	return nil
}

func (s *recordingEmailSender) SendSignupOnExistingAccount(_ context.Context, _ domain.EmailAddress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.existingNotices++
	return nil
}

func (s *recordingEmailSender) lastVerification(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.verifications) == 0 {
		t.Fatal("expected a verification email")
	}
	return s.verifications[len(s.verifications)-1]
}

func (s *recordingEmailSender) lastReset(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.resets) == 0 {
		t.Fatal("expected a password reset email")
	}
	return s.resets[len(s.resets)-1]
}

func (s *recordingEmailSender) counts() (verifications, resets, notices int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.verifications), len(s.resets), s.existingNotices
}

func newEmailAuthStack(t *testing.T) *emailAuthStack {
	t.Helper()
	connStr := testDatabaseURL(t, "whole-stack email authentication tests")
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
		`TRUNCATE room_photo_slots, room_sculptures, sculptures, rooms, museums, assets, email_outbox, password_credentials, pending_signups, password_resets, auth_attempts, external_identities, accounts CASCADE`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	signingKey := []byte("integration-signing-key-that-is-long-enough")
	accessTokens := identityinfra.NewAccessTokenSigner(signingKey, "muse-backend", 15*time.Minute)
	sessions := identityinfra.NewInMemorySessionStore()

	accountRepo := identityinfra.NewPostgresAccountRepository(pool.Pool())
	accountService := identityapp.NewAccountService(accountRepo)
	login := identityapp.NewLoginService(
		nil, accountService, sessions, accessTokens,
		identityinfra.OpaqueRefreshTokenGenerator{}, 180*24*time.Hour, 30*24*time.Hour,
	)
	refresh := identityapp.NewRefreshService(sessions, accessTokens, identityinfra.OpaqueRefreshTokenGenerator{}, 30*24*time.Hour)
	logout := identityapp.NewLogoutService(sessions)

	email := &recordingEmailSender{}
	limiter := identityinfra.NewPostgresAttemptLimiter(pool.Pool(), identityinfra.DefaultAttemptPolicy)
	hasher := identityinfra.NewArgon2idHasher(identityinfra.Argon2idParams{
		Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})

	passwords := identityapp.NewPasswordService(
		identityinfra.NewPostgresPasswordRepository(pool.Pool()),
		identityinfra.NewPostgresPendingSignupRepository(pool.Pool()),
		identityinfra.NewPostgresPasswordResetRepository(pool.Pool()),
		accountRepo, sessions, login,
		hasher, identityinfra.OpaqueRefreshTokenGenerator{}, email, limiter,
		identityinfra.NewPostgresEmailOutbox(pool.Pool()),
	)

	router := platformhttp.NewRouter()
	identityiface.NewHandlers(login, refresh, logout, accountService, passwords, accessTokens, logger).RegisterRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return &emailAuthStack{server: server, pool: pool, email: email, limiter: limiter, sessions: sessions, passwords: passwords}
}

func (s *emailAuthStack) post(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := testPostJSON(s.server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	decoded := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	return resp.StatusCode, decoded
}

func (s *emailAuthStack) signUpAndVerify(t *testing.T, email, password string) map[string]any {
	t.Helper()
	if status, _ := s.post(t, "/auth/email/signup", map[string]string{
		"email": email, "password": password,
	}); status != http.StatusAccepted {
		t.Fatalf("signup returned %d", status)
	}
	status, body := s.post(t, "/auth/email/verify", map[string]string{
		"token": s.email.lastVerification(t),
	})
	if status != http.StatusOK {
		t.Fatalf("verify returned %d: %v", status, body)
	}
	return body
}

func TestEmailAuth_SignUpReturnsNoSessionAndCreatesNoAccount(t *testing.T) {
	s := newEmailAuthStack(t)

	status, body := s.post(t, "/auth/email/signup", map[string]string{
		"email": "new@example.com", "password": "a-good-passphrase",
	})

	if status != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", status)
	}
	if _, present := body["access_token"]; present {
		t.Fatal(" is verify-first: sign-up must not return a session")
	}
	if body["status"] != "verification_sent" {
		t.Fatalf("unexpected body: %v", body)
	}

	var accounts int
	if err := s.pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM accounts`).Scan(&accounts); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if accounts != 0 {
		t.Fatalf("no account may exist before verification, found %d", accounts)
	}
}

func TestEmailAuth_VerificationCreatesTheAccountAndIssuesASession(t *testing.T) {
	s := newEmailAuthStack(t)

	body := s.signUpAndVerify(t, "new@example.com", "a-good-passphrase")

	if body["access_token"] == "" || body["refresh_token"] == "" {
		t.Fatalf("verification must return a full session: %v", body)
	}
	if body["is_new_account"] != true {
		t.Fatal("the account is created at verification, so this is a new account")
	}

	var accounts, credentials int
	ctx := context.Background()
	_ = s.pool.Pool().QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&accounts)
	_ = s.pool.Pool().QueryRow(ctx, `SELECT count(*) FROM password_credentials`).Scan(&credentials)
	if accounts != 1 || credentials != 1 {
		t.Fatalf("expected exactly one account and one credential, got %d/%d", accounts, credentials)
	}
}

func TestEmailAuth_SessionRefreshesThroughTheSharedEndpoint(t *testing.T) {
	s := newEmailAuthStack(t)
	session := s.signUpAndVerify(t, "new@example.com", "a-good-passphrase")

	status, refreshed := s.post(t, "/auth/refresh", map[string]string{
		"refresh_token": session["refresh_token"].(string),
	})

	if status != http.StatusOK {
		t.Fatalf("a password-originated session must refresh like any other, got %d", status)
	}
	if refreshed["refresh_token"] == session["refresh_token"] {
		t.Fatal("refresh must rotate the token, exactly as for a provider session")
	}
}

func TestEmailAuth_VerificationTokenIsSingleUse(t *testing.T) {
	s := newEmailAuthStack(t)
	if status, _ := s.post(t, "/auth/email/signup", map[string]string{
		"email": "new@example.com", "password": "a-good-passphrase",
	}); status != http.StatusAccepted {
		t.Fatal("signup failed")
	}
	token := s.email.lastVerification(t)
	if status, _ := s.post(t, "/auth/email/verify", map[string]string{"token": token}); status != http.StatusOK {
		t.Fatal("first verification failed")
	}

	status, _ := s.post(t, "/auth/email/verify", map[string]string{"token": token})

	if status != http.StatusBadRequest {
		t.Fatalf("a replayed verification token must be refused, got %d", status)
	}
	var accounts int
	_ = s.pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM accounts`).Scan(&accounts)
	if accounts != 1 {
		t.Fatalf("a replay must not create a second account, found %d", accounts)
	}
}

func TestEmailAuth_SignUpResponseIsIdenticalForTakenAndFreshAddresses(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "taken@example.com", "a-good-passphrase")

	takenStatus, takenBody := s.post(t, "/auth/email/signup", map[string]string{
		"email": "taken@example.com", "password": "another-passphrase",
	})
	freshStatus, freshBody := s.post(t, "/auth/email/signup", map[string]string{
		"email": "fresh@example.com", "password": "another-passphrase",
	})

	if takenStatus != freshStatus {
		t.Fatalf("status differs: taken=%d fresh=%d — that is an account-existence oracle", takenStatus, freshStatus)
	}
	takenJSON, _ := json.Marshal(takenBody)
	freshJSON, _ := json.Marshal(freshBody)
	if string(takenJSON) != string(freshJSON) {
		t.Fatalf("body differs:\n taken=%s\n fresh=%s", takenJSON, freshJSON)
	}
	if _, _, notices := s.email.counts(); notices != 1 {
		t.Fatalf("expected one existing-account notice, got %d", notices)
	}
}

func TestEmailAuth_LoginResponseIsIdenticalForUnknownAddressAndWrongPassword(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "known@example.com", "the-real-passphrase")

	unknownStatus, unknownBody := s.post(t, "/auth/email/login", map[string]string{
		"email": "stranger@example.com", "password": "any-passphrase",
	})
	wrongStatus, wrongBody := s.post(t, "/auth/email/login", map[string]string{
		"email": "known@example.com", "password": "wrong-passphrase",
	})

	if unknownStatus != wrongStatus {
		t.Fatalf("status differs: unknown=%d wrong=%d", unknownStatus, wrongStatus)
	}
	if unknownStatus != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unknownStatus)
	}
	unknownJSON, _ := json.Marshal(unknownBody)
	wrongJSON, _ := json.Marshal(wrongBody)
	if string(unknownJSON) != string(wrongJSON) {
		t.Fatalf("body differs:\n unknown=%s\n wrong=%s", unknownJSON, wrongJSON)
	}
}

func TestEmailAuth_PasswordResetResponseIsIdenticalForKnownAndUnknownAddresses(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "known@example.com", "the-real-passphrase")

	knownStatus, knownBody := s.post(t, "/auth/email/password-reset", map[string]string{
		"email": "known@example.com",
	})
	unknownStatus, unknownBody := s.post(t, "/auth/email/password-reset", map[string]string{
		"email": "stranger@example.com",
	})

	if knownStatus != unknownStatus {
		t.Fatalf("status differs: known=%d unknown=%d — the classic enumeration oracle", knownStatus, unknownStatus)
	}
	knownJSON, _ := json.Marshal(knownBody)
	unknownJSON, _ := json.Marshal(unknownBody)
	if string(knownJSON) != string(unknownJSON) {
		t.Fatalf("body differs:\n known=%s\n unknown=%s", knownJSON, unknownJSON)
	}
	s.drain(t)
	if _, resets, _ := s.email.counts(); resets != 1 {
		t.Fatalf("expected exactly one reset email, got %d", resets)
	}
}

func TestEmailAuth_ResendResponseIsIdenticalForKnownAndUnknownAddresses(t *testing.T) {
	s := newEmailAuthStack(t)
	if status, _ := s.post(t, "/auth/email/signup", map[string]string{
		"email": "pending@example.com", "password": "a-good-passphrase",
	}); status != http.StatusAccepted {
		t.Fatal("signup failed")
	}

	knownStatus, knownBody := s.post(t, "/auth/email/verification/resend", map[string]string{
		"email": "pending@example.com",
	})
	unknownStatus, unknownBody := s.post(t, "/auth/email/verification/resend", map[string]string{
		"email": "stranger@example.com",
	})

	if knownStatus != unknownStatus {
		t.Fatalf("status differs: known=%d unknown=%d", knownStatus, unknownStatus)
	}
	knownJSON, _ := json.Marshal(knownBody)
	unknownJSON, _ := json.Marshal(unknownBody)
	if string(knownJSON) != string(unknownJSON) {
		t.Fatalf("body differs:\n known=%s\n unknown=%s", knownJSON, unknownJSON)
	}
}

func TestEmailAuth_LoginSucceedsAfterVerification(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "user@example.com", "the-real-passphrase")

	status, body := s.post(t, "/auth/email/login", map[string]string{
		"email": "user@example.com", "password": "the-real-passphrase",
	})

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["access_token"] == "" {
		t.Fatal("a successful login must return a session")
	}
	if body["is_new_account"] != false {
		t.Fatal("logging in to an existing account is not a new account")
	}
}

func TestEmailAuth_CannotLogInBeforeVerifying(t *testing.T) {
	s := newEmailAuthStack(t)
	if status, _ := s.post(t, "/auth/email/signup", map[string]string{
		"email": "pending@example.com", "password": "a-good-passphrase",
	}); status != http.StatusAccepted {
		t.Fatal("signup failed")
	}

	status, _ := s.post(t, "/auth/email/login", map[string]string{
		"email": "pending@example.com", "password": "a-good-passphrase",
	})

	if status != http.StatusUnauthorized {
		t.Fatalf("an unverified sign-up must not be able to log in, got %d", status)
	}
}

func TestEmailAuth_WeakPasswordIsRejectedWithGuidance(t *testing.T) {
	s := newEmailAuthStack(t)

	status, body := s.post(t, "/auth/email/signup", map[string]string{
		"email": "new@example.com", "password": "short",
	})

	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
	message, _ := body["error"].(string)
	if !strings.Contains(message, "password") {
		t.Fatalf("the user must be told what to change, got %q", message)
	}
}

func TestEmailAuth_InvalidEmailIsRejected(t *testing.T) {
	s := newEmailAuthStack(t)

	status, _ := s.post(t, "/auth/email/signup", map[string]string{
		"email": "not-an-address", "password": "a-good-passphrase",
	})

	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestEmailAuth_RepeatedFailuresAreThrottled(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "user@example.com", "the-real-passphrase")

	var lastStatus int
	for i := 0; i < identityinfra.DefaultAttemptPolicy.MaxFailures+1; i++ {
		lastStatus, _ = s.post(t, "/auth/email/login", map[string]string{
			"email": "user@example.com", "password": "wrong-passphrase",
		})
	}

	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after %d failures, got %d", identityinfra.DefaultAttemptPolicy.MaxFailures, lastStatus)
	}
	status, _ := s.post(t, "/auth/email/login", map[string]string{
		"email": "user@example.com", "password": "the-real-passphrase",
	})
	if status != http.StatusTooManyRequests {
		t.Fatalf("a locked account must stay locked, got %d", status)
	}
}

func TestEmailAuth_ThrottleIsPersisted(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "user@example.com", "the-real-passphrase")
	for i := 0; i < identityinfra.DefaultAttemptPolicy.MaxFailures; i++ {
		s.post(t, "/auth/email/login", map[string]string{
			"email": "user@example.com", "password": "wrong-passphrase",
		})
	}

	var count int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT failure_count FROM auth_attempts WHERE scope = 'login' AND locked_until IS NOT NULL LIMIT 1`,
	).Scan(&count); err != nil {
		t.Fatalf("the lockout must be recorded in the database: %v", err)
	}
	if count < identityinfra.DefaultAttemptPolicy.MaxFailures {
		t.Fatalf("recorded %d failures, expected at least %d", count, identityinfra.DefaultAttemptPolicy.MaxFailures)
	}
}

func TestEmailAuth_ResetChangesThePasswordAndRevokesEverySession(t *testing.T) {
	s := newEmailAuthStack(t)
	first := s.signUpAndVerify(t, "user@example.com", "the-original-passphrase")
	_, second := s.post(t, "/auth/email/login", map[string]string{
		"email": "user@example.com", "password": "the-original-passphrase",
	})

	if status, _ := s.post(t, "/auth/email/password-reset", map[string]string{
		"email": "user@example.com",
	}); status != http.StatusAccepted {
		t.Fatal("reset request failed")
	}
	s.drain(t)
	status, _ := s.post(t, "/auth/email/password-reset/confirm", map[string]string{
		"token": s.email.lastReset(t), "password": "a-brand-new-passphrase",
	})
	if status != http.StatusOK {
		t.Fatalf("reset confirm returned %d", status)
	}

	if code, _ := s.post(t, "/auth/email/login", map[string]string{
		"email": "user@example.com", "password": "a-brand-new-passphrase",
	}); code != http.StatusOK {
		t.Fatalf("the new password must work, got %d", code)
	}
	if code, _ := s.post(t, "/auth/email/login", map[string]string{
		"email": "user@example.com", "password": "the-original-passphrase",
	}); code != http.StatusUnauthorized {
		t.Fatalf("the old password must stop working, got %d", code)
	}
	for name, session := range map[string]map[string]any{
		"the session from verification": first,
		"a later login's session":       second,
	} {
		code, _ := s.post(t, "/auth/refresh", map[string]string{
			"refresh_token": session["refresh_token"].(string),
		})
		if code != http.StatusUnauthorized {
			t.Fatalf("%s must be revoked by the reset, refresh returned %d", name, code)
		}
	}
}

func TestEmailAuth_ResetConfirmReturnsNoSession(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "user@example.com", "the-original-passphrase")
	s.post(t, "/auth/email/password-reset", map[string]string{"email": "user@example.com"})
	s.drain(t)

	_, body := s.post(t, "/auth/email/password-reset/confirm", map[string]string{
		"token": s.email.lastReset(t), "password": "a-brand-new-passphrase",
	})

	if _, present := body["access_token"]; present {
		t.Fatalf("the reset must not hand out a session: %v", body)
	}
}

func TestEmailAuth_ResetTokenIsSingleUse(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "user@example.com", "the-original-passphrase")
	s.post(t, "/auth/email/password-reset", map[string]string{"email": "user@example.com"})
	s.drain(t)
	token := s.email.lastReset(t)
	if status, _ := s.post(t, "/auth/email/password-reset/confirm", map[string]string{
		"token": token, "password": "a-brand-new-passphrase",
	}); status != http.StatusOK {
		t.Fatal("first reset failed")
	}

	status, _ := s.post(t, "/auth/email/password-reset/confirm", map[string]string{
		"token": token, "password": "an-attackers-passphrase",
	})

	if status != http.StatusBadRequest {
		t.Fatalf("a replayed reset token must be refused, got %d", status)
	}
	if code, _ := s.post(t, "/auth/email/login", map[string]string{
		"email": "user@example.com", "password": "a-brand-new-passphrase",
	}); code != http.StatusOK {
		t.Fatal("the first reset's password must still be in force")
	}
}

func TestEmailAuth_DatabaseHoldsNoPlaintextCredential(t *testing.T) {
	s := newEmailAuthStack(t)
	const password = "a-very-distinctive-passphrase"
	s.signUpAndVerify(t, "user@example.com", password)
	s.post(t, "/auth/email/password-reset", map[string]string{"email": "user@example.com"})
	s.drain(t)
	resetToken := s.email.lastReset(t)
	ctx := context.Background()

	var storedHash string
	if err := s.pool.Pool().QueryRow(ctx, `SELECT password_hash FROM password_credentials LIMIT 1`).Scan(&storedHash); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if strings.Contains(storedHash, password) {
		t.Fatal("the stored hash must not contain the password")
	}
	if !strings.HasPrefix(storedHash, "$argon2id$") {
		t.Fatalf("expected an Argon2id PHC hash, got %q", storedHash)
	}

	var storedDigest string
	if err := s.pool.Pool().QueryRow(ctx, `SELECT token_digest FROM password_resets LIMIT 1`).Scan(&storedDigest); err != nil {
		t.Fatalf("read digest: %v", err)
	}
	if storedDigest == resetToken {
		t.Fatal("the raw reset token must never be stored")
	}
	if storedDigest != domain.DigestOpaqueToken(resetToken) {
		t.Fatal("the stored value must be the token's digest")
	}
}

func TestEmailAuth_StoredHashCarriesItsParameters(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "user@example.com", "a-good-passphrase")

	var storedHash string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT password_hash FROM password_credentials LIMIT 1`).Scan(&storedHash); err != nil {
		t.Fatalf("read hash: %v", err)
	}

	if !strings.Contains(storedHash, "m=") || !strings.Contains(storedHash, "t=") || !strings.Contains(storedHash, "p=") {
		t.Fatalf("the stored hash must record its parameters, got %q", storedHash)
	}
}

func TestEmailAuth_NeverLinksToAProviderAccountWithTheSameEmail(t *testing.T) {
	s := newEmailAuthStack(t)
	ctx := context.Background()

	var providerAccountID string
	if err := s.pool.Pool().QueryRow(ctx,
		`INSERT INTO accounts (display_name) VALUES ('provider user') RETURNING id`).Scan(&providerAccountID); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := s.pool.Pool().Exec(ctx,
		`INSERT INTO external_identities (account_id, provider, subject)
		 VALUES ($1, 'apple', 'apple-subject-1')`, providerAccountID); err != nil {
		t.Fatalf("seed identity: %v", err)
	}

	session := s.signUpAndVerify(t, "shared@example.com", "a-good-passphrase")

	var credentialAccountID string
	if err := s.pool.Pool().QueryRow(ctx,
		`SELECT account_id FROM password_credentials WHERE email = 'shared@example.com'`).Scan(&credentialAccountID); err != nil {
		t.Fatalf("read credential: %v", err)
	}
	if credentialAccountID == providerAccountID {
		t.Fatal("a password sign-up must NEVER attach to a provider account with the same email address")
	}
	if session["access_token"] == "" {
		t.Fatal("the separate account must still get its own session")
	}

	var accounts int
	_ = s.pool.Pool().QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&accounts)
	if accounts != 2 {
		t.Fatalf("expected two distinct accounts, found %d", accounts)
	}
}

func TestEmailAuth_ProviderRoutesStillPresentAndIndependent(t *testing.T) {
	s := newEmailAuthStack(t)

	for _, path := range []string{"/auth/apple", "/auth/google"} {
		status, _ := s.post(t, path, map[string]string{"identity_token": ""})
		if status == http.StatusNotFound {
			t.Fatalf("%s must still be routed", path)
		}
		if status != http.StatusBadRequest {
			t.Fatalf("%s must still perform its own validation, got %d", path, status)
		}
	}
}

func TestEmailAuth_ConcurrentVerificationYieldsOneAccount(t *testing.T) {
	s := newEmailAuthStack(t)
	if status, _ := s.post(t, "/auth/email/signup", map[string]string{
		"email": "race@example.com", "password": "a-good-passphrase",
	}); status != http.StatusAccepted {
		t.Fatal("signup failed")
	}
	token := s.email.lastVerification(t)

	const racers = 4
	statuses := make([]int, racers)
	var wg sync.WaitGroup
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(index int) {
			defer wg.Done()
			statuses[index], _ = s.post(t, "/auth/email/verify", map[string]string{"token": token})
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, status := range statuses {
		if status == http.StatusOK {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("exactly one verification must succeed, %d did", successes)
	}
	var accounts int
	_ = s.pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM accounts`).Scan(&accounts)
	if accounts != 1 {
		t.Fatalf("expected exactly one account, found %d", accounts)
	}
}
