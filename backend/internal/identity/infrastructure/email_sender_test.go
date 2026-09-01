package infrastructure_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"muse-backend/internal/identity/infrastructure"
)

func captureResendRequest(t *testing.T, send func(*infrastructure.ResendEmailSender) error) (map[string]any, http.Header) {
	t.Helper()

	var (
		captured map[string]any
		headers  http.Header
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"stub"}`))
	}))
	t.Cleanup(server.Close)

	sender := infrastructure.NewResendEmailSender("test-api-key", "muse@example.com", "https://muse.example", nil)
	sender.SetEndpoint(server.URL)
	if err := send(sender); err != nil {
		t.Fatalf("send: %v", err)
	}
	return captured, headers
}

func TestResendEmailSender_VerificationRequestShape(t *testing.T) {
	body, headers := captureResendRequest(t, func(s *infrastructure.ResendEmailSender) error {
		return s.SendEmailVerification(context.Background(), "user@example.com", "the-token")
	})

	if got := headers.Get("Authorization"); got != "Bearer test-api-key" {
		t.Fatalf("Authorization header is %q", got)
	}
	if got := headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type is %q", got)
	}
	if body["from"] != "muse@example.com" {
		t.Fatalf("from is %v", body["from"])
	}
	recipients, ok := body["to"].([]any)
	if !ok || len(recipients) != 1 || recipients[0] != "user@example.com" {
		t.Fatalf("to is %v", body["to"])
	}
	subject, _ := body["subject"].(string)
	if !strings.Contains(strings.ToLower(subject), "verify") {
		t.Fatalf("subject %q does not describe verification", subject)
	}
}

func TestResendEmailSender_TokenAppearsOnlyInTheBody(t *testing.T) {
	const token = "a-very-distinctive-token"
	body, _ := captureResendRequest(t, func(s *infrastructure.ResendEmailSender) error {
		return s.SendEmailVerification(context.Background(), "user@example.com", token)
	})

	subject, _ := body["subject"].(string)
	text, _ := body["text"].(string)

	if strings.Contains(subject, token) {
		t.Fatal("the token must never appear in the subject line")
	}
	if !strings.Contains(text, token) {
		t.Fatal("the body must carry the token, or the link is useless")
	}
}

func TestResendEmailSender_ResetRequestMentionsSignOut(t *testing.T) {
	body, _ := captureResendRequest(t, func(s *infrastructure.ResendEmailSender) error {
		return s.SendPasswordReset(context.Background(), "user@example.com", "the-token")
	})

	text, _ := body["text"].(string)
	if !strings.Contains(strings.ToLower(text), "signs you out") {
		t.Fatalf("a reset revokes every session; the email must say so. Got: %q", text)
	}
}

func TestResendEmailSender_ExistingAccountNoticeCarriesNoToken(t *testing.T) {
	body, _ := captureResendRequest(t, func(s *infrastructure.ResendEmailSender) error {
		return s.SendSignupOnExistingAccount(context.Background(), "user@example.com")
	})

	text, _ := body["text"].(string)
	if strings.Contains(text, "token=") {
		t.Fatal("the existing-account notice must not contain a credential link")
	}
	if !strings.Contains(strings.ToLower(text), "forgot password") {
		t.Fatalf("the notice must point at recovery. Got: %q", text)
	}
}

func TestResendEmailSender_ErrorDoesNotEchoTheProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	sender := infrastructure.NewResendEmailSender("k", "muse@example.com", "https://muse.example", nil)
	sender.SetEndpoint(server.URL)

	err := sender.SendEmailVerification(context.Background(), "user@example.com", "secret-token-value")

	if err == nil {
		t.Fatal("a non-2xx response must be an error")
	}
	if strings.Contains(err.Error(), "secret-token-value") {
		t.Fatalf("the error must not carry the token: %q", err)
	}
}

func TestLogEmailSender_NeverLogsTheTokenOrTheAddress(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	sender := infrastructure.NewLogEmailSender(logger, "https://muse.example")

	if err := sender.SendEmailVerification(context.Background(), "someone@example.com", "a-secret-token"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := sender.SendPasswordReset(context.Background(), "someone@example.com", "another-secret"); err != nil {
		t.Fatalf("send: %v", err)
	}

	output := logged.String()
	for _, forbidden := range []string{"a-secret-token", "another-secret", "someone@example.com"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("the log must not contain %q. Got: %s", forbidden, output)
		}
	}
	if !strings.Contains(output, "example.com") {
		t.Fatalf("expected the recipient domain to be logged for diagnostics. Got: %s", output)
	}
	if !strings.Contains(output, "non-production") {
		t.Fatalf("the log must state that this is not a real sender. Got: %s", output)
	}
}

func TestLogEmailSender_ExposesTheTokenOnlyThroughTheDevAccessor(t *testing.T) {
	sender := infrastructure.NewLogEmailSender(slog.New(slog.NewTextHandler(io.Discard, nil)), "https://muse.example")

	if err := sender.SendPasswordReset(context.Background(), "someone@example.com", "the-token"); err != nil {
		t.Fatalf("send: %v", err)
	}

	if got := sender.LastTokenForLocalDevelopment(); got != "the-token" {
		t.Fatalf("dev accessor returned %q", got)
	}
}
