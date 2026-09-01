package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
)

type countingHasher struct {
	inner *infrastructure.Argon2idHasher

	mu       sync.Mutex
	hashes   int
	verifies int
	decoys   int
}

func (c *countingHasher) Hash(password string) (string, error) {
	c.mu.Lock()
	c.hashes++
	c.mu.Unlock()
	return c.inner.Hash(password)
}

func (c *countingHasher) Verify(password, encoded string) (bool, bool, error) {
	c.mu.Lock()
	c.verifies++
	c.mu.Unlock()
	return c.inner.Verify(password, encoded)
}

func (c *countingHasher) VerifyDecoy(password string) {
	c.mu.Lock()
	c.decoys++
	c.mu.Unlock()
	c.inner.VerifyDecoy(password)
}

func (c *countingHasher) derivations() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.verifies + c.decoys
}

func (c *countingHasher) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hashes, c.verifies, c.decoys = 0, 0, 0
}

func newCountingHarness(t *testing.T) (*passwordHarness, *countingHasher) {
	t.Helper()

	spy := &countingHasher{inner: testHasher()}
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
		spy, infrastructure.OpaqueRefreshTokenGenerator{}, email, limiter, outbox,
	)

	return &passwordHarness{
		service: service, credentials: credentials, pending: pending, resets: resets,
		accounts: accounts, sessions: sessions, email: email, limiter: limiter, login: login,
		outbox: outbox,
	}, spy
}

func TestLogIn_UnknownAddressPerformsTheSameHashingWorkAsAWrongPassword(t *testing.T) {
	h, spy := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")

	spy.reset()
	_, wrongErr := h.service.LogIn(context.Background(), "known@example.com", "wrong-passphrase", "source")
	wrongPasswordWork := spy.derivations()

	spy.reset()
	_, unknownErr := h.service.LogIn(context.Background(), "stranger@example.com", "wrong-passphrase", "source")
	unknownAddressWork := spy.derivations()

	if !errors.Is(wrongErr, domain.ErrInvalidPassword) {
		t.Fatalf("precondition: expected ErrInvalidPassword, got %v", wrongErr)
	}
	if !errors.Is(unknownErr, domain.ErrCredentialNotFound) {
		t.Fatalf("precondition: expected ErrCredentialNotFound, got %v", unknownErr)
	}

	if wrongPasswordWork != 1 {
		t.Fatalf("a wrong password must cost exactly one derivation, cost %d", wrongPasswordWork)
	}
	if unknownAddressWork != wrongPasswordWork {
		t.Fatalf(
			"timing oracle: unknown address cost %d derivation(s), wrong password cost %d — "+
				"an attacker can tell which addresses have accounts by response time",
			unknownAddressWork, wrongPasswordWork,
		)
	}
}

func TestLogIn_UnknownAddressUsesTheDecoyPath(t *testing.T) {
	h, spy := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	spy.reset()

	_, _ = h.service.LogIn(context.Background(), "stranger@example.com", "any-passphrase", "source")

	if spy.decoys != 1 {
		t.Fatalf("expected exactly one decoy verification, got %d", spy.decoys)
	}
	if spy.verifies != 0 {
		t.Fatalf("an unknown address must never verify against a real credential, but Verify ran %d time(s)", spy.verifies)
	}
	if spy.hashes != 0 {
		t.Fatalf("login must never *hash* — that is sign-up's job — but Hash ran %d time(s)", spy.hashes)
	}
}

func TestLogIn_KnownAddressNeverUsesTheDecoy(t *testing.T) {
	h, spy := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	spy.reset()

	_, _ = h.service.LogIn(context.Background(), "known@example.com", "wrong-passphrase", "source")
	_, _ = h.service.LogIn(context.Background(), "known@example.com", "the-real-passphrase", "source")

	if spy.decoys != 0 {
		t.Fatalf("a known address must go through a real verification only; decoy ran %d time(s)", spy.decoys)
	}
	if spy.verifies != 2 {
		t.Fatalf("expected two real verifications, got %d", spy.verifies)
	}
}

func TestLogIn_UnknownAddressStillCountsAgainstBothThrottleKeys(t *testing.T) {
	h, _ := newCountingHarness(t)
	unknownKey := domain.DigestOpaqueToken("stranger@example.com")

	_, _ = h.service.LogIn(context.Background(), "stranger@example.com", "any-passphrase", "source-key")

	if h.limiter.failureCount(application.AttemptScopeLogin, unknownKey) != 1 {
		t.Fatal("the unknown address must accumulate a failure — constant work must not weaken rate limiting")
	}
	if h.limiter.failureCount(application.AttemptScopeLogin, "source-key") != 1 {
		t.Fatal("the source must accumulate a failure for an unknown address too")
	}
}

func TestLogIn_ThrottledUnknownAddressSkipsTheDecoyToo(t *testing.T) {
	h, spy := newCountingHarness(t)
	h.limiter.lock(application.AttemptScopeLogin, domain.DigestOpaqueToken("stranger@example.com"))
	spy.reset()

	_, err := h.service.LogIn(context.Background(), "stranger@example.com", "any-passphrase", "source")

	if !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("expected ErrTooManyAttempts, got %v", err)
	}
	if spy.derivations() != 0 {
		t.Fatalf("a throttled request must cost no derivation at all, cost %d", spy.derivations())
	}
}

func TestLogIn_DecoyPathNeverAuthenticates(t *testing.T) {
	h, _ := newCountingHarness(t)

	for _, password := range []string{
		"a-good-passphrase",
		"muse-decoy-credential-that-belongs-to-no-account-e3f1a9",
		"",
	} {
		result, err := h.service.LogIn(context.Background(), "stranger@example.com", password, "source")
		if err == nil || result.AccessToken != "" {
			t.Fatalf("password %q against an unknown address must never yield a session", password)
		}
		if !errors.Is(err, domain.ErrCredentialNotFound) {
			t.Fatalf("expected ErrCredentialNotFound for %q, got %v", password, err)
		}
	}
}
