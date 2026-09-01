package interfaces_test

import (
	"net/http"
	"testing"
)

func TestEmailEndpoints_WithoutADatabase_Return503(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	cases := map[string]struct {
		path string
		body any
	}{
		"signup": {"/auth/email/signup", map[string]string{
			"email": "someone@example.com", "password": "a-good-passphrase",
		}},
		"verify": {"/auth/email/verify", map[string]string{"token": "anything"}},
		"resend": {"/auth/email/verification/resend", map[string]string{
			"email": "someone@example.com",
		}},
		"login": {"/auth/email/login", map[string]string{
			"email": "someone@example.com", "password": "a-good-passphrase",
		}},
		"password reset request": {"/auth/email/password-reset", map[string]string{
			"email": "someone@example.com",
		}},
		"password reset confirm": {"/auth/email/password-reset/confirm", map[string]string{
			"token": "anything", "password": "a-good-passphrase",
		}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postJSON(t, server.URL+tc.path, tc.body)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("expected 503 without a database, got %d", resp.StatusCode)
			}
		})
	}
}

func TestProviderEndpoints_StillRespondWhenEmailAuthIsUnavailable(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	for name, path := range map[string]string{
		"apple":  "/auth/apple",
		"google": "/auth/google",
	} {
		t.Run(name, func(t *testing.T) {
			resp := postJSON(t, server.URL+path, map[string]string{"identity_token": ""})
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusNotFound {
				t.Fatalf("provider sign-in must be unaffected by email auth being unavailable, got %d", resp.StatusCode)
			}
		})
	}
}
