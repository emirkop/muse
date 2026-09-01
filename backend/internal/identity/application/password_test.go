package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
)

func testHasher() *infrastructure.Argon2idHasher {
	return infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
}

type fakeCredentialRepo struct {
	mu               sync.Mutex
	byEmail          map[domain.EmailAddress]domain.PasswordCredential
	byAccount        map[domain.AccountID]domain.PasswordCredential
	nextID           int
	createCalls      int
	findByEmailCalls int
}

func newFakeCredentialRepo() *fakeCredentialRepo {
	return &fakeCredentialRepo{
		byEmail:   map[domain.EmailAddress]domain.PasswordCredential{},
		byAccount: map[domain.AccountID]domain.PasswordCredential{},
	}
}

func (f *fakeCredentialRepo) CreateAccountWithCredential(_ context.Context, account domain.Account, credential domain.PasswordCredential) (domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if _, exists := f.byEmail[credential.Email]; exists {
		return domain.Account{}, domain.ErrEmailAlreadyRegistered
	}
	f.nextID++
	account.ID = domain.AccountID("account-" + string(rune('0'+f.nextID)))
	credential.AccountID = account.ID
	f.byEmail[credential.Email] = credential
	f.byAccount[account.ID] = credential
	return account, nil
}

func (f *fakeCredentialRepo) FindByEmail(_ context.Context, email domain.EmailAddress) (domain.PasswordCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findByEmailCalls++
	credential, ok := f.byEmail[email]
	if !ok {
		return domain.PasswordCredential{}, domain.ErrCredentialNotFound
	}
	return credential, nil
}

func (f *fakeCredentialRepo) FindByAccountID(_ context.Context, id domain.AccountID) (domain.PasswordCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	credential, ok := f.byAccount[id]
	if !ok {
		return domain.PasswordCredential{}, domain.ErrCredentialNotFound
	}
	return credential, nil
}

func (f *fakeCredentialRepo) UpdateHash(_ context.Context, id domain.AccountID, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	credential, ok := f.byAccount[id]
	if !ok {
		return domain.ErrCredentialNotFound
	}
	credential.Hash = hash
	f.byAccount[id] = credential
	f.byEmail[credential.Email] = credential
	return nil
}

type fakePendingRepo struct {
	mu               sync.Mutex
	byEmail          map[domain.EmailAddress]domain.PendingSignup
	byDigest         map[string]domain.PendingSignup
	findByEmailCalls int
}

func newFakePendingRepo() *fakePendingRepo {
	return &fakePendingRepo{
		byEmail:  map[domain.EmailAddress]domain.PendingSignup{},
		byDigest: map[string]domain.PendingSignup{},
	}
}

func (f *fakePendingRepo) Upsert(_ context.Context, signup domain.PendingSignup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if previous, ok := f.byEmail[signup.Email]; ok {
		delete(f.byDigest, previous.TokenDigest)
	}
	f.byEmail[signup.Email] = signup
	f.byDigest[signup.TokenDigest] = signup
	return nil
}

func (f *fakePendingRepo) FindByTokenDigest(_ context.Context, digest string) (domain.PendingSignup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	signup, ok := f.byDigest[digest]
	if !ok {
		return domain.PendingSignup{}, domain.ErrVerificationTokenInvalid
	}
	return signup, nil
}

func (f *fakePendingRepo) FindByEmail(_ context.Context, email domain.EmailAddress) (domain.PendingSignup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findByEmailCalls++
	signup, ok := f.byEmail[email]
	if !ok {
		return domain.PendingSignup{}, domain.ErrVerificationTokenInvalid
	}
	return signup, nil
}

func (f *fakePendingRepo) Consume(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for digest, signup := range f.byDigest {
		if signup.ID != id {
			continue
		}
		if signup.ConsumedAt != nil {
			return domain.ErrVerificationTokenInvalid
		}
		now := time.Now()
		signup.ConsumedAt = &now
		f.byDigest[digest] = signup
		f.byEmail[signup.Email] = signup
		return nil
	}
	return domain.ErrVerificationTokenInvalid
}

type fakeResetRepo struct {
	mu       sync.Mutex
	byDigest map[string]domain.PasswordReset
}

func newFakeResetRepo() *fakeResetRepo {
	return &fakeResetRepo{byDigest: map[string]domain.PasswordReset{}}
}

func (f *fakeResetRepo) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byDigest)
}

func (f *fakeResetRepo) Create(_ context.Context, reset domain.PasswordReset) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byDigest[reset.TokenDigest] = reset
	return nil
}

func (f *fakeResetRepo) FindByTokenDigest(_ context.Context, digest string) (domain.PasswordReset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reset, ok := f.byDigest[digest]
	if !ok {
		return domain.PasswordReset{}, domain.ErrResetTokenInvalid
	}
	return reset, nil
}

func (f *fakeResetRepo) Consume(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for digest, reset := range f.byDigest {
		if reset.ID != id {
			continue
		}
		if reset.ConsumedAt != nil {
			return domain.ErrResetTokenInvalid
		}
		now := time.Now()
		reset.ConsumedAt = &now
		f.byDigest[digest] = reset
		return nil
	}
	return domain.ErrResetTokenInvalid
}

func (f *fakeResetRepo) InvalidateAllForAccount(_ context.Context, accountID domain.AccountID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for digest, reset := range f.byDigest {
		if reset.AccountID == accountID && reset.ConsumedAt == nil {
			reset.ConsumedAt = &now
			f.byDigest[digest] = reset
		}
	}
	return nil
}

type fakeEmailSender struct {
	mu               sync.Mutex
	verifications    []sentEmail
	resets           []sentEmail
	existingNotices  []domain.EmailAddress
	failNextDelivery bool
	failAll          bool
}

type sentEmail struct {
	To    domain.EmailAddress
	Token string
}

func (f *fakeEmailSender) SendEmailVerification(_ context.Context, to domain.EmailAddress, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errors.New("delivery failed")
	}
	if f.failNextDelivery {
		f.failNextDelivery = false
		return errors.New("delivery failed")
	}
	f.verifications = append(f.verifications, sentEmail{To: to, Token: token})
	return nil
}

func (f *fakeEmailSender) SendPasswordReset(_ context.Context, to domain.EmailAddress, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errors.New("delivery failed")
	}
	if f.failNextDelivery {
		f.failNextDelivery = false
		return errors.New("delivery failed")
	}
	f.resets = append(f.resets, sentEmail{To: to, Token: token})
	return nil
}

func (f *fakeEmailSender) SendSignupOnExistingAccount(_ context.Context, to domain.EmailAddress) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.existingNotices = append(f.existingNotices, to)
	return nil
}

func (f *fakeEmailSender) lastVerificationToken(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.verifications) == 0 {
		t.Fatal("expected a verification email to have been sent")
	}
	return f.verifications[len(f.verifications)-1].Token
}

func (f *fakeEmailSender) lastResetToken(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.resets) == 0 {
		t.Fatal("expected a password reset email to have been sent")
	}
	return f.resets[len(f.resets)-1].Token
}

type fakeLimiter struct {
	mu       sync.Mutex
	failures map[string]int
	locked   map[string]bool
	resets   map[string]int
}

func newFakeLimiter() *fakeLimiter {
	return &fakeLimiter{
		failures: map[string]int{},
		locked:   map[string]bool{},
		resets:   map[string]int{},
	}
}

func (f *fakeLimiter) key(scope application.AttemptScope, key string) string {
	return string(scope) + "|" + key
}

func (f *fakeLimiter) Check(_ context.Context, scope application.AttemptScope, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locked[f.key(scope, key)] {
		return domain.ErrTooManyAttempts
	}
	return nil
}

func (f *fakeLimiter) RecordFailure(_ context.Context, scope application.AttemptScope, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[f.key(scope, key)]++
	return nil
}

func (f *fakeLimiter) Reset(_ context.Context, scope application.AttemptScope, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets[f.key(scope, key)]++
	delete(f.failures, f.key(scope, key))
	return nil
}

func (f *fakeLimiter) lock(scope application.AttemptScope, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.locked[f.key(scope, key)] = true
}

func (f *fakeLimiter) failureCount(scope application.AttemptScope, key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failures[f.key(scope, key)]
}

type passwordHarness struct {
	service     *application.PasswordService
	credentials *fakeCredentialRepo
	pending     *fakePendingRepo
	resets      *fakeResetRepo
	accounts    *fakeAccountRepository
	sessions    *infrastructure.InMemorySessionStore
	email       *fakeEmailSender
	limiter     *fakeLimiter
	login       *application.LoginService
	outbox      *fakeOutbox
}

func (h *passwordHarness) drain(t *testing.T) application.DrainReport {
	t.Helper()
	report, err := h.service.DrainEmailOutbox(context.Background())
	if err != nil {
		t.Fatalf("drain outbox: %v", err)
	}
	return report
}

func newPasswordHarness(t *testing.T) *passwordHarness {
	t.Helper()

	credentials := newFakeCredentialRepo()
	pending := newFakePendingRepo()
	resets := newFakeResetRepo()
	accounts := newFakeAccountRepository()
	sessions := infrastructure.NewInMemorySessionStore()
	email := &fakeEmailSender{}
	limiter := newFakeLimiter()
	outbox := newFakeOutbox()

	login := application.NewLoginService(
		nil, nil, sessions,
		infrastructure.NewAccessTokenSigner([]byte("test-signing-key-at-least-32-bytes!!"), "muse-test", time.Minute),
		infrastructure.OpaqueRefreshTokenGenerator{},
		24*time.Hour, time.Hour,
	)

	service := application.NewPasswordService(
		credentials, pending, resets, accounts, sessions, login,
		testHasher(), infrastructure.OpaqueRefreshTokenGenerator{}, email, limiter, outbox,
	)

	return &passwordHarness{
		service: service, credentials: credentials, pending: pending, resets: resets,
		accounts: accounts, sessions: sessions, email: email, limiter: limiter, login: login,
		outbox: outbox,
	}
}

func (h *passwordHarness) signUpAndVerify(t *testing.T, email, password string) application.SessionResult {
	t.Helper()
	if err := h.service.SignUp(context.Background(), email, password, "source"); err != nil {
		t.Fatalf("sign up: %v", err)
	}
	token := h.email.lastVerificationToken(t)
	result, err := h.service.VerifyEmail(context.Background(), token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	credential, err := h.credentials.FindByEmail(context.Background(), domain.EmailAddress(strings.ToLower(email)))
	if err != nil {
		t.Fatalf("credential must exist after verification: %v", err)
	}
	h.accounts.accountsByID[credential.AccountID] = domain.Account{ID: credential.AccountID}
	return result
}

func TestSignUp_CreatesNoAccountAndNoSession(t *testing.T) {
	h := newPasswordHarness(t)

	if err := h.service.SignUp(context.Background(), "new@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("sign up: %v", err)
	}

	if h.credentials.createCalls != 0 {
		t.Fatal(" is verify-first: sign-up must not create an account")
	}
	if _, err := h.credentials.FindByEmail(context.Background(), "new@example.com"); !errors.Is(err, domain.ErrCredentialNotFound) {
		t.Fatal("no credential may exist before verification")
	}
	if len(h.email.verifications) != 1 {
		t.Fatalf("expected exactly one verification email, got %d", len(h.email.verifications))
	}
}

func TestSignUp_StoresOnlyAHashAndOnlyATokenDigest(t *testing.T) {
	h := newPasswordHarness(t)
	const password = "a-good-passphrase"

	if err := h.service.SignUp(context.Background(), "new@example.com", password, "source"); err != nil {
		t.Fatalf("sign up: %v", err)
	}

	stored, err := h.pending.FindByEmail(context.Background(), "new@example.com")
	if err != nil {
		t.Fatalf("pending signup must exist: %v", err)
	}
	if strings.Contains(stored.PasswordHash, password) {
		t.Fatal("the stored value must not contain the password")
	}
	rawToken := h.email.lastVerificationToken(t)
	if stored.TokenDigest == rawToken {
		t.Fatal("the raw token must never be stored — only its digest")
	}
	if stored.TokenDigest != domain.DigestOpaqueToken(rawToken) {
		t.Fatal("the stored digest must match the emailed token's digest")
	}
}

func TestVerifyEmail_CreatesTheAccountAndIssuesTheFirstSession(t *testing.T) {
	h := newPasswordHarness(t)

	result := h.signUpAndVerify(t, "new@example.com", "a-good-passphrase")

	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatal("verification must issue a full session")
	}
	if !result.IsNewAccount {
		t.Fatal("the account is created at verification, so this login is a new account")
	}
	if h.credentials.createCalls != 1 {
		t.Fatalf("expected exactly one account creation, got %d", h.credentials.createCalls)
	}
}

func TestVerifyEmail_TokenIsSingleUse(t *testing.T) {
	h := newPasswordHarness(t)
	if err := h.service.SignUp(context.Background(), "new@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("sign up: %v", err)
	}
	token := h.email.lastVerificationToken(t)

	if _, err := h.service.VerifyEmail(context.Background(), token); err != nil {
		t.Fatalf("first verification: %v", err)
	}
	_, err := h.service.VerifyEmail(context.Background(), token)

	if !errors.Is(err, domain.ErrVerificationTokenInvalid) {
		t.Fatalf("a verification token must work exactly once, got %v", err)
	}
	if h.credentials.createCalls != 1 {
		t.Fatalf("a replayed token must not create a second account, got %d creations", h.credentials.createCalls)
	}
}

func TestVerifyEmail_RejectsUnknownAndEmptyTokens(t *testing.T) {
	h := newPasswordHarness(t)

	for name, token := range map[string]string{"empty": "", "unknown": "not-a-real-token"} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.service.VerifyEmail(context.Background(), token); !errors.Is(err, domain.ErrVerificationTokenInvalid) {
				t.Fatalf("expected ErrVerificationTokenInvalid, got %v", err)
			}
		})
	}
}

func TestVerifyEmail_RejectsAnExpiredToken(t *testing.T) {
	h := newPasswordHarness(t)
	if err := h.service.SignUp(context.Background(), "new@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("sign up: %v", err)
	}
	token := h.email.lastVerificationToken(t)

	h.service.SetClock(func() time.Time { return time.Now().Add(application.VerificationTokenTTL + time.Hour) })

	if _, err := h.service.VerifyEmail(context.Background(), token); !errors.Is(err, domain.ErrVerificationTokenInvalid) {
		t.Fatalf("an expired verification token must be refused, got %v", err)
	}
}

func TestResendVerification_InvalidatesThePreviousLink(t *testing.T) {
	h := newPasswordHarness(t)
	if err := h.service.SignUp(context.Background(), "new@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("sign up: %v", err)
	}
	firstToken := h.email.lastVerificationToken(t)

	if err := h.service.ResendVerification(context.Background(), "new@example.com", "source"); err != nil {
		t.Fatalf("resend: %v", err)
	}
	h.drain(t)
	secondToken := h.email.lastVerificationToken(t)

	if firstToken == secondToken {
		t.Fatal("a resend must issue a fresh token")
	}
	if _, err := h.service.VerifyEmail(context.Background(), firstToken); !errors.Is(err, domain.ErrVerificationTokenInvalid) {
		t.Fatalf("the superseded link must stop working, got %v", err)
	}
	if _, err := h.service.VerifyEmail(context.Background(), secondToken); err != nil {
		t.Fatalf("the newest link must work, got %v", err)
	}
}

func TestResendVerification_IsNeutralForAnUnknownAddress(t *testing.T) {
	h := newPasswordHarness(t)

	if err := h.service.ResendVerification(context.Background(), "stranger@example.com", "source"); err != nil {
		t.Fatalf("resend for an unknown address must not error (it would be an oracle): %v", err)
	}
	h.drain(t)
	if len(h.email.verifications) != 0 {
		t.Fatal("nothing should be sent for an address with no outstanding signup")
	}
}

func TestSignUp_ExistingAddressIsIndistinguishableButNotified(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "taken@example.com", "a-good-passphrase")
	h.email.verifications = nil

	err := h.service.SignUp(context.Background(), "taken@example.com", "another-passphrase", "source")

	if err != nil {
		t.Fatalf("sign-up on an existing address must return the same success as a new one, got %v", err)
	}
	if len(h.email.verifications) != 0 {
		t.Fatal("no verification email may be sent for an address that is already registered")
	}
	if len(h.email.existingNotices) != 1 {
		t.Fatalf("the mailbox owner must be told an account already exists, got %d notices", len(h.email.existingNotices))
	}
	if h.credentials.createCalls != 1 {
		t.Fatal("the existing account's credential must not be touched")
	}
}

func TestSignUp_DoesNotOverwriteAnExistingCredential(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "taken@example.com", "the-original-passphrase")
	before, _ := h.credentials.FindByEmail(context.Background(), "taken@example.com")

	if err := h.service.SignUp(context.Background(), "taken@example.com", "an-attackers-passphrase", "source"); err != nil {
		t.Fatalf("sign up: %v", err)
	}

	after, _ := h.credentials.FindByEmail(context.Background(), "taken@example.com")
	if after.Hash != before.Hash {
		t.Fatal("a sign-up attempt must never change an existing account's password")
	}
	if _, err := h.service.LogIn(context.Background(), "taken@example.com", "the-original-passphrase", "source"); err != nil {
		t.Fatalf("the original password must still log in: %v", err)
	}
	if _, err := h.service.LogIn(context.Background(), "taken@example.com", "an-attackers-passphrase", "source"); err == nil {
		t.Fatal("the attempted password must not work")
	}
}

func TestLogIn_UnknownAddressAndWrongPasswordAreSeparateErrorsInternallyOnly(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")

	_, unknownErr := h.service.LogIn(context.Background(), "stranger@example.com", "any-passphrase", "source")
	_, wrongErr := h.service.LogIn(context.Background(), "known@example.com", "wrong-passphrase", "source")

	if !errors.Is(unknownErr, domain.ErrCredentialNotFound) {
		t.Fatalf("expected ErrCredentialNotFound, got %v", unknownErr)
	}
	if !errors.Is(wrongErr, domain.ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", wrongErr)
	}
}

func TestRequestPasswordReset_IsNeutralForAnUnknownAddress(t *testing.T) {
	h := newPasswordHarness(t)

	err := h.service.RequestPasswordReset(context.Background(), "stranger@example.com", "source")

	if err != nil {
		t.Fatalf("a reset request for an unknown address must succeed silently, got %v", err)
	}
	h.drain(t)
	if len(h.email.resets) != 0 {
		t.Fatal("nothing may be sent for an address with no credential")
	}
}

func TestRequestPasswordReset_CountsAttemptsForUnknownAddressesToo(t *testing.T) {
	h := newPasswordHarness(t)
	unknownKey := domain.DigestOpaqueToken("stranger@example.com")

	if err := h.service.RequestPasswordReset(context.Background(), "stranger@example.com", "source"); err != nil {
		t.Fatalf("reset request: %v", err)
	}

	if h.limiter.failureCount(application.AttemptScopePasswordReset, unknownKey) == 0 {
		t.Fatal("an unknown address must be counted, or throttling reveals which addresses exist")
	}
}

func TestRequestPasswordReset_InvalidAddressIsSilentlyAccepted(t *testing.T) {
	h := newPasswordHarness(t)

	if err := h.service.RequestPasswordReset(context.Background(), "not-an-address", "source"); err != nil {
		t.Fatalf("an unparseable address must not produce a distinguishable error, got %v", err)
	}
}

func TestLogIn_SucceedsWithTheCorrectPassword(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-real-passphrase")

	result, err := h.service.LogIn(context.Background(), "user@example.com", "the-real-passphrase", "source")

	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatal("a successful login must issue a full session")
	}
	if result.IsNewAccount {
		t.Fatal("logging in to an existing account is not a new account")
	}
}

func TestLogIn_IsCaseInsensitiveOnTheAddress(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-real-passphrase")

	if _, err := h.service.LogIn(context.Background(), "  User@Example.COM ", "the-real-passphrase", "source"); err != nil {
		t.Fatalf("a differently-cased address must log in: %v", err)
	}
}

func TestLogIn_RefusesADeactivatedAccount(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-real-passphrase")
	credential, _ := h.credentials.FindByEmail(context.Background(), "user@example.com")
	deleted := time.Now()
	h.accounts.accountsByID[credential.AccountID] = domain.Account{ID: credential.AccountID, DeletedAt: &deleted}

	_, err := h.service.LogIn(context.Background(), "user@example.com", "the-real-passphrase", "source")

	if !errors.Is(err, domain.ErrAccountDeactivated) {
		t.Fatalf("a deactivated account must not authenticate, got %v", err)
	}
}

func TestLogIn_ThrottleBlocksBeforeTouchingTheCredential(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-real-passphrase")
	h.limiter.lock(application.AttemptScopeLogin, domain.DigestOpaqueToken("user@example.com"))

	_, err := h.service.LogIn(context.Background(), "user@example.com", "the-real-passphrase", "source")

	if !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("a locked key must be refused even with the correct password, got %v", err)
	}
}

func TestLogIn_FailureCountsAgainstBothTheAddressAndTheSource(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-real-passphrase")
	emailKey := domain.DigestOpaqueToken("user@example.com")

	if _, err := h.service.LogIn(context.Background(), "user@example.com", "wrong", "source-key"); err == nil {
		t.Fatal("expected the login to fail")
	}

	if h.limiter.failureCount(application.AttemptScopeLogin, emailKey) != 1 {
		t.Fatal("the address must accumulate failures, or a botnet can grind one account")
	}
	if h.limiter.failureCount(application.AttemptScopeLogin, "source-key") != 1 {
		t.Fatal("the source must accumulate failures, or one host can spray many addresses")
	}
}

func TestLogIn_SuccessClearsTheCounter(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-real-passphrase")
	emailKey := domain.DigestOpaqueToken("user@example.com")
	if _, err := h.service.LogIn(context.Background(), "user@example.com", "wrong", "source"); err == nil {
		t.Fatal("expected failure")
	}

	if _, err := h.service.LogIn(context.Background(), "user@example.com", "the-real-passphrase", "source"); err != nil {
		t.Fatalf("login: %v", err)
	}

	if h.limiter.failureCount(application.AttemptScopeLogin, emailKey) != 0 {
		t.Fatal("a successful login must clear the counter — a user who mistyped twice must not stay throttled")
	}
}

func TestLogIn_RehashesAWeakStoredHash(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-real-passphrase")

	weak := infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 512, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	weakHash, err := weak.Hash("the-real-passphrase")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	credential, _ := h.credentials.FindByEmail(context.Background(), "user@example.com")
	if err := h.credentials.UpdateHash(context.Background(), credential.AccountID, weakHash); err != nil {
		t.Fatalf("seed weak hash: %v", err)
	}

	if _, err := h.service.LogIn(context.Background(), "user@example.com", "the-real-passphrase", "source"); err != nil {
		t.Fatalf("a weak-but-valid hash must still log in: %v", err)
	}

	upgraded, _ := h.credentials.FindByEmail(context.Background(), "user@example.com")
	if upgraded.Hash == weakHash {
		t.Fatal("the stored hash must be upgraded after a successful login")
	}
	if !strings.Contains(upgraded.Hash, "m=1024") {
		t.Fatalf("the upgrade must use the current parameters, got %q", upgraded.Hash)
	}
}

func TestLogIn_DoesNotRehashOnAFailedAttempt(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-real-passphrase")
	before, _ := h.credentials.FindByEmail(context.Background(), "user@example.com")

	if _, err := h.service.LogIn(context.Background(), "user@example.com", "wrong-passphrase", "source"); err == nil {
		t.Fatal("expected failure")
	}

	after, _ := h.credentials.FindByEmail(context.Background(), "user@example.com")
	if after.Hash != before.Hash {
		t.Fatal("a failed login must not touch the stored hash")
	}
}

func TestResetPassword_ChangesThePasswordAndRevokesEverySession(t *testing.T) {
	h := newPasswordHarness(t)
	first := h.signUpAndVerify(t, "user@example.com", "the-original-passphrase")
	second, err := h.service.LogIn(context.Background(), "user@example.com", "the-original-passphrase", "source")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	if err := h.service.RequestPasswordReset(context.Background(), "user@example.com", "source"); err != nil {
		t.Fatalf("reset request: %v", err)
	}
	h.drain(t)
	token := h.email.lastResetToken(t)
	if err := h.service.ResetPassword(context.Background(), token, "a-brand-new-passphrase"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if _, err := h.service.LogIn(context.Background(), "user@example.com", "a-brand-new-passphrase", "source"); err != nil {
		t.Fatalf("the new password must work: %v", err)
	}
	if _, err := h.service.LogIn(context.Background(), "user@example.com", "the-original-passphrase", "source"); !errors.Is(err, domain.ErrInvalidPassword) {
		t.Fatalf("the old password must stop working, got %v", err)
	}

	for name, refreshToken := range map[string]string{
		"session created at verification":  first.RefreshToken,
		"session created by a later login": second.RefreshToken,
	} {
		refreshService := application.NewRefreshService(
			h.sessions,
			infrastructure.NewAccessTokenSigner([]byte("test-signing-key-at-least-32-bytes!!"), "muse-test", time.Minute),
			infrastructure.OpaqueRefreshTokenGenerator{}, time.Hour,
		)
		if _, err := refreshService.Refresh(context.Background(), refreshToken); err == nil {
			t.Fatalf("%s must be revoked by the reset", name)
		}
	}
}

func TestResetPassword_TokenIsSingleUse(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-original-passphrase")
	if err := h.service.RequestPasswordReset(context.Background(), "user@example.com", "source"); err != nil {
		t.Fatalf("reset request: %v", err)
	}
	h.drain(t)
	token := h.email.lastResetToken(t)

	if err := h.service.ResetPassword(context.Background(), token, "a-brand-new-passphrase"); err != nil {
		t.Fatalf("first reset: %v", err)
	}
	err := h.service.ResetPassword(context.Background(), token, "yet-another-passphrase")

	if !errors.Is(err, domain.ErrResetTokenInvalid) {
		t.Fatalf("a reset token must work exactly once, got %v", err)
	}
	if _, err := h.service.LogIn(context.Background(), "user@example.com", "a-brand-new-passphrase", "source"); err != nil {
		t.Fatalf("the first reset's password must still be in force: %v", err)
	}
}

func TestResetPassword_InvalidatesOtherOutstandingTokens(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-original-passphrase")
	if err := h.service.RequestPasswordReset(context.Background(), "user@example.com", "source"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	h.drain(t)
	firstToken := h.email.lastResetToken(t)
	if err := h.service.RequestPasswordReset(context.Background(), "user@example.com", "source"); err != nil {
		t.Fatalf("second request: %v", err)
	}
	h.drain(t)
	secondToken := h.email.lastResetToken(t)

	if err := h.service.ResetPassword(context.Background(), secondToken, "a-brand-new-passphrase"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if err := h.service.ResetPassword(context.Background(), firstToken, "an-attackers-passphrase"); !errors.Is(err, domain.ErrResetTokenInvalid) {
		t.Fatalf("an earlier outstanding token must be dead after a reset, got %v", err)
	}
}

func TestResetPassword_RejectsAnExpiredToken(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-original-passphrase")
	if err := h.service.RequestPasswordReset(context.Background(), "user@example.com", "source"); err != nil {
		t.Fatalf("reset request: %v", err)
	}
	h.drain(t)
	token := h.email.lastResetToken(t)

	h.service.SetClock(func() time.Time { return time.Now().Add(application.PasswordResetTokenTTL + time.Minute) })

	if err := h.service.ResetPassword(context.Background(), token, "a-brand-new-passphrase"); !errors.Is(err, domain.ErrResetTokenInvalid) {
		t.Fatalf("an expired reset token must be refused, got %v", err)
	}
}

func TestResetPassword_EnforcesThePasswordPolicy(t *testing.T) {
	h := newPasswordHarness(t)
	h.signUpAndVerify(t, "user@example.com", "the-original-passphrase")
	if err := h.service.RequestPasswordReset(context.Background(), "user@example.com", "source"); err != nil {
		t.Fatalf("reset request: %v", err)
	}
	h.drain(t)
	token := h.email.lastResetToken(t)

	if err := h.service.ResetPassword(context.Background(), token, "short"); !errors.Is(err, domain.ErrWeakPassword) {
		t.Fatalf("the reset flow must apply the same policy as sign-up, got %v", err)
	}
	if err := h.service.ResetPassword(context.Background(), token, "a-valid-passphrase"); err != nil {
		t.Fatalf("the token must survive a policy rejection: %v", err)
	}
}

func TestResetPassword_RejectsUnknownTokens(t *testing.T) {
	h := newPasswordHarness(t)

	for name, token := range map[string]string{"empty": "", "unknown": "not-a-real-token"} {
		t.Run(name, func(t *testing.T) {
			if err := h.service.ResetPassword(context.Background(), token, "a-valid-passphrase"); !errors.Is(err, domain.ErrResetTokenInvalid) {
				t.Fatalf("expected ErrResetTokenInvalid, got %v", err)
			}
		})
	}
}

func TestSignUp_NeverLinksToAProviderAccountWithTheSameEmail(t *testing.T) {
	h := newPasswordHarness(t)

	providerAccountID := domain.AccountID("provider-account")
	h.accounts.accountsByID[providerAccountID] = domain.Account{ID: providerAccountID}

	result := h.signUpAndVerify(t, "shared@example.com", "a-good-passphrase")

	credential, err := h.credentials.FindByEmail(context.Background(), "shared@example.com")
	if err != nil {
		t.Fatalf("the password credential must exist: %v", err)
	}
	if credential.AccountID == providerAccountID {
		t.Fatal("a password sign-up must NEVER attach to a provider account with the same email")
	}
	if result.AccessToken == "" {
		t.Fatal("the new, separate account must still get its own session")
	}
}

func TestLogIn_HasNoEmailFallbackToProviderAccounts(t *testing.T) {
	h := newPasswordHarness(t)
	providerAccountID := domain.AccountID("provider-account")
	h.accounts.accountsByID[providerAccountID] = domain.Account{ID: providerAccountID}

	_, err := h.service.LogIn(context.Background(), "shared@example.com", "any-passphrase", "source")

	if !errors.Is(err, domain.ErrCredentialNotFound) {
		t.Fatalf("with no password credential, login must simply fail, got %v", err)
	}
}

func TestPasswordSession_RefreshesThroughTheSameService(t *testing.T) {
	h := newPasswordHarness(t)
	result := h.signUpAndVerify(t, "user@example.com", "a-good-passphrase")

	refreshService := application.NewRefreshService(
		h.sessions,
		infrastructure.NewAccessTokenSigner([]byte("test-signing-key-at-least-32-bytes!!"), "muse-test", time.Minute),
		infrastructure.OpaqueRefreshTokenGenerator{}, time.Hour,
	)
	refreshed, err := refreshService.Refresh(context.Background(), result.RefreshToken)

	if err != nil {
		t.Fatalf("a password-originated session must refresh like any other: %v", err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == result.RefreshToken {
		t.Fatal("refresh must rotate the token, exactly as it does for a provider session")
	}
}

func TestSignUp_ValidatesInputBeforeDoingAnyWork(t *testing.T) {
	h := newPasswordHarness(t)

	if err := h.service.SignUp(context.Background(), "not-an-address", "a-good-passphrase", "source"); !errors.Is(err, domain.ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
	if err := h.service.SignUp(context.Background(), "ok@example.com", "short", "source"); !errors.Is(err, domain.ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword, got %v", err)
	}
	if len(h.email.verifications) != 0 {
		t.Fatal("invalid input must not send any email")
	}
}

func TestSignUp_SurfacesADeliveryFailure(t *testing.T) {
	h := newPasswordHarness(t)
	h.email.failNextDelivery = true

	err := h.service.SignUp(context.Background(), "new@example.com", "a-good-passphrase", "source")

	if err == nil {
		t.Fatal("a failed delivery must be reported — the user would otherwise wait for an email that never comes")
	}
}
