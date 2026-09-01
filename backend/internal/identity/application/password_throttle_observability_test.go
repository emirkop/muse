package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
)

type failingLimiter struct {
	writeErr error
	writes   int
}

func (f *failingLimiter) Check(context.Context, application.AttemptScope, string) error { return nil }

func (f *failingLimiter) RecordFailure(context.Context, application.AttemptScope, string) error {
	f.writes++
	return f.writeErr
}

func (f *failingLimiter) Reset(context.Context, application.AttemptScope, string) error {
	f.writes++
	return f.writeErr
}

func captureLogs() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func (h *passwordHarness) serviceWith(
	t *testing.T,
	limiter application.AttemptLimiter,
	logger *slog.Logger,
) *application.PasswordService {
	t.Helper()
	return application.NewPasswordService(
		h.credentials, h.pending, h.resets, h.accounts, h.sessions, h.login,
		testHasher(), infrastructure.OpaqueRefreshTokenGenerator{}, h.email, limiter, h.outbox,
	).WithLogger(logger)
}

func (h *passwordHarness) seedCredential(t *testing.T, email, password string) {
	t.Helper()
	h.signUpAndVerify(t, email, password)
}

func TestThrottleWriteFailure_IsObservable(t *testing.T) {
	h := newPasswordHarness(t)
	limiter := &failingLimiter{writeErr: errors.New("auth_attempts insert failed")}
	logger, logs := captureLogs()
	service := h.serviceWith(t, limiter, logger)

	h.seedCredential(t, "throttle@example.com", "correct horse battery staple")
	_, err := service.LogIn(context.Background(), "throttle@example.com", "wrong password", "source")
	if !errors.Is(err, domain.ErrInvalidPassword) {
		t.Fatalf("login with a wrong password → %v, want ErrInvalidPassword", err)
	}
	if limiter.writes == 0 {
		t.Fatal("the throttle write was never attempted — this test would prove nothing")
	}

	if !strings.Contains(logs.String(), "authn.throttle_write_failed") {
		t.Fatalf("a failed throttle write must be logged; got:\n%s", logs.String())
	}
}

func TestThrottleWriteFailure_DoesNotChangeAuthOutcome(t *testing.T) {
	const email = "unchanged@example.com"
	const password = "correct horse battery staple"

	outcomes := map[string]struct {
		wrongPassword error
		goodPassword  error
	}{}

	for _, mode := range []string{"healthy writes", "failing writes"} {
		h := newPasswordHarness(t)
		var limiter application.AttemptLimiter
		if mode == "healthy writes" {
			limiter = newFakeLimiter()
		} else {
			limiter = &failingLimiter{writeErr: errors.New("auth_attempts insert failed")}
		}
		logger, _ := captureLogs()
		service := h.serviceWith(t, limiter, logger)
		h.seedCredential(t, email, password)

		_, wrongErr := service.LogIn(context.Background(), email, "wrong password", "source")
		_, goodErr := service.LogIn(context.Background(), email, password, "source")
		outcomes[mode] = struct {
			wrongPassword error
			goodPassword  error
		}{wrongErr, goodErr}
	}

	healthy, failing := outcomes["healthy writes"], outcomes["failing writes"]
	if !errors.Is(healthy.wrongPassword, domain.ErrInvalidPassword) ||
		!errors.Is(failing.wrongPassword, domain.ErrInvalidPassword) {
		t.Errorf("a wrong password must be refused identically: healthy=%v failing=%v",
			healthy.wrongPassword, failing.wrongPassword)
	}
	if healthy.goodPassword != nil || failing.goodPassword != nil {
		t.Errorf("a correct password must succeed either way: healthy=%v failing=%v",
			healthy.goodPassword, failing.goodPassword)
	}
}

func TestThrottleWriteFailure_LogsNoSensitiveInput(t *testing.T) {
	const email = "secret.person@example.com"
	const password = "correct horse battery staple"
	const source = "203.0.113.7"

	h := newPasswordHarness(t)
	limiter := &failingLimiter{writeErr: errors.New("auth_attempts insert failed")}
	logger, logs := captureLogs()
	service := h.serviceWith(t, limiter, logger)
	h.seedCredential(t, email, password)

	if _, err := service.LogIn(context.Background(), email, "wrong password", source); err == nil {
		t.Fatal("precondition: the login should have been refused")
	}
	if err := service.RequestPasswordReset(context.Background(), email, source); err != nil {
		t.Fatalf("reset request: %v", err)
	}

	output := logs.String()
	if !strings.Contains(output, "authn.throttle_write_failed") {
		t.Fatalf("precondition: nothing was logged, so this proves nothing:\n%s", output)
	}
	forbidden := map[string]string{
		"the address":        email,
		"its local part":     "secret.person",
		"the domain":         "example.com",
		"the password":       password,
		"the source address": source,
		"the address digest": domain.DigestOpaqueToken(email),
		"the source digest":  domain.DigestOpaqueToken(source),
	}
	for name, value := range forbidden {
		if strings.Contains(output, value) {
			t.Errorf("%s (%q) appears in the diagnostics:\n%s", name, value, output)
		}
	}

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if !strings.Contains(line, "authn.throttle_write_failed") {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("log line is not JSON: %v", err)
		}
		allowed := map[string]bool{
			"time": true, "level": true, "msg": true,
			"event": true, "category": true, "outcome": true, "error": true,
			"request_id": true, "account_id": true, "reason": true,
		}
		for key := range fields {
			if !allowed[key] {
				t.Errorf("unexpected field %q on the throttle line: %s", key, line)
			}
		}
	}
}
