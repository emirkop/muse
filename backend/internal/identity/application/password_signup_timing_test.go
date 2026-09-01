package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
)

func TestSignUp_TakenAddressPerformsTheSameKDFWorkAsAFreshOne(t *testing.T) {
	h, spy := newCountingHarness(t)
	h.signUpAndVerify(t, "taken@example.com", "the-original-passphrase")

	spy.reset()
	if err := h.service.SignUp(context.Background(), "fresh@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("fresh sign-up: %v", err)
	}
	freshWork := spy.hashes

	spy.reset()
	if err := h.service.SignUp(context.Background(), "taken@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("taken sign-up must return the same success a fresh one does, got %v", err)
	}
	takenWork := spy.hashes

	if freshWork != 1 {
		t.Fatalf("a fresh sign-up must cost exactly one derivation, cost %d", freshWork)
	}
	if takenWork != freshWork {
		t.Fatalf(
			"timing oracle: taken address cost %d derivation(s), fresh address cost %d — "+
				"an attacker can tell which addresses are registered by response time",
			takenWork, freshWork,
		)
	}
}

func TestSignUp_NeitherPathVerifies(t *testing.T) {
	h, spy := newCountingHarness(t)
	h.signUpAndVerify(t, "taken@example.com", "the-original-passphrase")
	spy.reset()

	_ = h.service.SignUp(context.Background(), "fresh@example.com", "a-good-passphrase", "source")
	_ = h.service.SignUp(context.Background(), "taken@example.com", "a-good-passphrase", "source")

	if spy.verifies != 0 || spy.decoys != 0 {
		t.Fatalf("sign-up must only Hash; saw %d Verify and %d VerifyDecoy calls", spy.verifies, spy.decoys)
	}
	if spy.hashes != 2 {
		t.Fatalf("expected exactly one Hash per sign-up, got %d for two sign-ups", spy.hashes)
	}
}

func TestSignUp_BothPathsMakeExactlyOneEmailCall(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "taken@example.com", "the-original-passphrase")
	h.email.verifications, h.email.existingNotices = nil, nil

	if err := h.service.SignUp(context.Background(), "fresh@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("fresh sign-up: %v", err)
	}
	freshCalls := len(h.email.verifications) + len(h.email.existingNotices)
	h.email.verifications, h.email.existingNotices = nil, nil

	if err := h.service.SignUp(context.Background(), "taken@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("taken sign-up: %v", err)
	}
	takenCalls := len(h.email.verifications) + len(h.email.existingNotices)

	if freshCalls != 1 || takenCalls != 1 {
		t.Fatalf("each path must make exactly one email call: fresh %d, taken %d", freshCalls, takenCalls)
	}
}

func TestSignUp_ThrottledRequestSkipsTheKDF(t *testing.T) {
	h, spy := newCountingHarness(t)
	h.limiter.lock(application.AttemptScopeSignup, domain.DigestOpaqueToken("anyone@example.com"))
	spy.reset()

	err := h.service.SignUp(context.Background(), "anyone@example.com", "a-good-passphrase", "source")

	if !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("expected ErrTooManyAttempts, got %v", err)
	}
	if spy.hashes != 0 {
		t.Fatalf("a throttled sign-up must cost no derivation, cost %d", spy.hashes)
	}
}

func TestSignUp_InvalidInputSkipsTheKDF(t *testing.T) {
	h, spy := newCountingHarness(t)
	spy.reset()

	_ = h.service.SignUp(context.Background(), "not-an-address", "a-good-passphrase", "source")
	_ = h.service.SignUp(context.Background(), "ok@example.com", "short", "source")

	if spy.hashes != 0 {
		t.Fatalf("validation must run before any derivation; cost %d", spy.hashes)
	}
}

func TestSignUp_TakenAddressOutcomeIsUnchangedByConstantWork(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "taken@example.com", "the-original-passphrase")
	before, _ := h.credentials.FindByEmail(context.Background(), "taken@example.com")
	pendingBefore, _ := h.pending.FindByEmail(context.Background(), "taken@example.com")
	h.email.verifications, h.email.existingNotices = nil, nil

	err := h.service.SignUp(context.Background(), "taken@example.com", "an-attackers-passphrase", "source")

	if err != nil {
		t.Fatalf("the taken path must still return the neutral success, got %v", err)
	}
	after, _ := h.credentials.FindByEmail(context.Background(), "taken@example.com")
	if after.Hash != before.Hash {
		t.Fatal("the discarded hash must never reach the existing credential")
	}
	if len(h.email.verifications) != 0 || len(h.email.existingNotices) != 1 {
		t.Fatalf("expected one existing-account notice and no verification email, got %d/%d",
			len(h.email.existingNotices), len(h.email.verifications))
	}
	pendingAfter, _ := h.pending.FindByEmail(context.Background(), "taken@example.com")
	if pendingAfter.TokenDigest != pendingBefore.TokenDigest || pendingAfter.PasswordHash != pendingBefore.PasswordHash {
		t.Fatal("a taken address must not create or refresh a pending sign-up")
	}
	if pendingAfter.ConsumedAt == nil {
		t.Fatal("the old, consumed pending row must not have been reopened")
	}
}
