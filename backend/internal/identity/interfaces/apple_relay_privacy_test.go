package interfaces_test

import (
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"muse-backend/internal/identity/infrastructure"
)

func signAppleTokenWithEmail(t *testing.T, priv *ecdsa.PrivateKey, kid, subject, email string, verified any) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": infrastructure.AppleIssuer,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": subject,
	}
	if email != "" {
		claims["email"] = email
		claims["email_verified"] = verified
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return signed
}

func TestPrivacy_AppleRelayRealAndAbsentEmails_AllAuthenticateIdentically(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)

	cases := map[string]struct {
		subject, email string
		verified       any
	}{
		"hide my email relay":       {"apple-relay-1", "a1b2c3d4e5@privaterelay.appleid.com", "true"},
		"relay, boolean verified":   {"apple-relay-2", "f6g7h8i9j0@privaterelay.appleid.com", true},
		"real address":              {"apple-real-1", "person@example.com", true},
		"no email claim at all":     {"apple-noemail-1", "", nil},
		"email present, unverified": {"apple-unverified-1", "person2@example.com", false},
	}

	type outcome struct {
		status                         int
		hasAccess, hasRefresh, isNewOK bool
	}
	results := map[string]outcome{}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			token := signAppleTokenWithEmail(t, priv, kid, tc.subject, tc.email, tc.verified)
			resp := postJSON(t, server.URL+"/auth/apple", map[string]string{"identity_token": token})
			defer resp.Body.Close()

			var body struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				IsNewAccount bool   `json:"is_new_account"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&body)

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s must authenticate exactly like any other address, got %d", name, resp.StatusCode)
			}
			if !body.IsNewAccount {
				t.Fatalf("%s: a first login provisions an account regardless of the email claim", name)
			}
			results[name] = outcome{resp.StatusCode, body.AccessToken != "", body.RefreshToken != "", body.IsNewAccount}
		})
	}

	var reference outcome
	var referenceName string
	for name, got := range results {
		if referenceName == "" {
			referenceName, reference = name, got
			continue
		}
		if got != reference {
			t.Fatalf("%s (%+v) differs from %s (%+v) — an email's shape must not change the outcome",
				name, got, referenceName, reference)
		}
	}
	if len(results) != len(cases) {
		t.Fatalf("only %d of %d cases ran", len(results), len(cases))
	}
}

func TestPrivacy_ProviderEmailIsNeverReadableThroughAnyIdentitySurface(t *testing.T) {
	server, priv, kid, accessTokens := setupTestServer(t)

	const relay = "zz9plural@privaterelay.appleid.com"
	token := signAppleTokenWithEmail(t, priv, kid, "apple-relay-readback", relay, "true")
	resp := postJSON(t, server.URL+"/auth/apple", map[string]string{"identity_token": token})
	var session struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&session)
	resp.Body.Close()
	if session.AccessToken == "" {
		t.Fatal("login did not return a session")
	}

	claims, err := accessTokens.Verify(session.AccessToken)
	if err != nil {
		t.Fatal(err)
	}

	surfaces := map[string]*http.Response{
		"own profile":         authenticatedRequest(t, http.MethodGet, server.URL+"/profile/me", session.AccessToken, nil),
		"own profile by id":   authenticatedRequest(t, http.MethodGet, server.URL+"/profile/"+string(claims.AccountID), session.AccessToken, nil),
		"profile after patch": authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", session.AccessToken, map[string]string{"display_name": "Relay User"}),
	}
	for name, r := range surfaces {
		func() {
			defer r.Body.Close()
			var raw map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Fatalf("%s: decode: %v", name, err)
			}
			for key := range raw {
				if strings.Contains(strings.ToLower(key), "email") {
					t.Errorf("%s exposes an email-shaped field %q", name, key)
				}
			}
			for _, needle := range []string{relay, "privaterelay", "@"} {
				for key, value := range raw {
					if strings.Contains(string(value), needle) {
						t.Errorf("%s: field %q leaks %q: %s", name, key, needle, value)
					}
				}
			}
		}()
	}

	if strings.Contains(session.AccessToken, "privaterelay") {
		t.Fatal("the access token must not embed the email address")
	}
}
