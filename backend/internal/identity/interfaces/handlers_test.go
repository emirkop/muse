package interfaces_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
	"muse-backend/internal/identity/interfaces"
	platformhttp "muse-backend/internal/platform/http"
)

const testAudience = "com.muse.app.test"

type testJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type fakeAccountRepository struct {
	mu                sync.Mutex
	accountsByID      map[domain.AccountID]domain.Account
	accountByIdentity map[string]domain.AccountID
	nextID            int
}

func newFakeAccountRepository() *fakeAccountRepository {
	return &fakeAccountRepository{
		accountsByID:      make(map[domain.AccountID]domain.Account),
		accountByIdentity: make(map[string]domain.AccountID),
	}
}

func fakeIdentityKey(provider domain.Provider, subject string) string {
	return string(provider) + ":" + subject
}

func (f *fakeAccountRepository) FindByLinkedIdentity(_ context.Context, provider domain.Provider, subject string) (domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	id, ok := f.accountByIdentity[fakeIdentityKey(provider, subject)]
	if !ok {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	return f.accountsByID[id], nil
}

func (f *fakeAccountRepository) CreateWithLinkedIdentity(_ context.Context, account domain.Account, identity domain.LinkedIdentity) (domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := fakeIdentityKey(identity.Provider, identity.Subject)
	if _, exists := f.accountByIdentity[key]; exists {
		return domain.Account{}, domain.ErrLinkedIdentityAlreadyExists
	}

	f.nextID++
	account.ID = domain.AccountID(fmt.Sprintf("acct_%d", f.nextID))
	f.accountsByID[account.ID] = account
	f.accountByIdentity[key] = account.ID
	return account, nil
}

func (f *fakeAccountRepository) FindByID(_ context.Context, id domain.AccountID) (domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	account, ok := f.accountsByID[id]
	if !ok {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	return account, nil
}

func (f *fakeAccountRepository) UpdateDisplayName(_ context.Context, id domain.AccountID, displayName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	account, ok := f.accountsByID[id]
	if !ok {
		return domain.ErrAccountNotFound
	}
	account.DisplayName = displayName
	f.accountsByID[id] = account
	return nil
}

func (f *fakeAccountRepository) UpdateAvatar(_ context.Context, id domain.AccountID, avatarID domain.AvatarID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	account, ok := f.accountsByID[id]
	if !ok {
		return domain.ErrAccountNotFound
	}
	account.AvatarID = avatarID
	f.accountsByID[id] = account
	return nil
}

func (f *fakeAccountRepository) Deactivate(_ context.Context, id domain.AccountID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	account, ok := f.accountsByID[id]
	if !ok {
		return domain.ErrAccountNotFound
	}
	now := time.Now()
	account.DeletedAt = &now
	f.accountsByID[id] = account
	return nil
}

func setupTestServer(t *testing.T) (*httptest.Server, *ecdsa.PrivateKey, string, *infrastructure.AccessTokenSigner) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}

	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Keys []testJWK `json:"keys"`
		}{Keys: []testJWK{{
			Kty: "EC",
			Kid: "test-kid",
			Crv: "P-256",
			X:   base64.RawURLEncoding.EncodeToString(priv.PublicKey.X.Bytes()),
			Y:   base64.RawURLEncoding.EncodeToString(priv.PublicKey.Y.Bytes()),
		}}})
	}))
	t.Cleanup(jwksServer.Close)

	jwks := infrastructure.NewJWKSClient(jwksServer.URL, nil, time.Hour)
	appleVerifier := infrastructure.NewAppleVerifier(testAudience, jwks)
	googleVerifier := infrastructure.NewGoogleVerifier(testAudience, jwks)
	providerVerifier := infrastructure.NewProviderVerifier(appleVerifier, googleVerifier)

	sessions := infrastructure.NewInMemorySessionStore()
	accountService := application.NewAccountService(newFakeAccountRepository())
	accessTokens := infrastructure.NewAccessTokenSigner([]byte("test-signing-key"), "muse-test", 15*time.Minute)
	refreshGen := infrastructure.OpaqueRefreshTokenGenerator{}

	login := application.NewLoginService(providerVerifier, accountService, sessions, accessTokens, refreshGen, 180*24*time.Hour, 30*24*time.Hour)
	refresh := application.NewRefreshService(sessions, accessTokens, refreshGen, 30*24*time.Hour)
	logout := application.NewLogoutService(sessions)

	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	handlers := interfaces.NewHandlers(login, refresh, logout, accountService, nil, accessTokens, logger)

	router := platformhttp.NewRouter()
	handlers.RegisterRoutes(router)

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server, priv, "test-kid", accessTokens
}

func signTestAppleToken(t *testing.T, priv *ecdsa.PrivateKey, kid, subject string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": infrastructure.AppleIssuer,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": subject,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return signed
}

func postJSON(t *testing.T, url string, body interface{}) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func authenticatedRequest(t *testing.T, method, url, accessToken string, body interface{}) *http.Response {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func loginAndGetAccessToken(t *testing.T, server *httptest.Server, priv *ecdsa.PrivateKey, kid, subject string) string {
	t.Helper()

	token := signTestAppleToken(t, priv, kid, subject)
	resp := postJSON(t, server.URL+"/auth/apple", map[string]string{"identity_token": token})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login for %q: expected status %d, got %d", subject, http.StatusOK, resp.StatusCode)
	}

	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return body.AccessToken
}

func TestHandlers_AppleLogin_Success(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	token := signTestAppleToken(t, priv, kid, "apple-subject-1")

	resp := postJSON(t, server.URL+"/auth/apple", map[string]string{"identity_token": token})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IsNewAccount bool   `json:"is_new_account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.AccessToken == "" || body.RefreshToken == "" {
		t.Fatal("expected non-empty access_token and refresh_token")
	}
	if !body.IsNewAccount {
		t.Error("expected is_new_account to be true for a never-seen identity's first login")
	}
}

func TestHandlers_AppleLogin_ExistingIdentity_IsNewAccountFalse(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	token := signTestAppleToken(t, priv, kid, "apple-subject-returning")

	first := postJSON(t, server.URL+"/auth/apple", map[string]string{"identity_token": token})
	defer first.Body.Close()
	var firstBody struct {
		IsNewAccount bool `json:"is_new_account"`
	}
	if err := json.NewDecoder(first.Body).Decode(&firstBody); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if !firstBody.IsNewAccount {
		t.Fatal("expected the first login for this identity to report is_new_account true")
	}

	second := postJSON(t, server.URL+"/auth/apple", map[string]string{"identity_token": token})
	defer second.Body.Close()
	var secondBody struct {
		IsNewAccount bool `json:"is_new_account"`
	}
	if err := json.NewDecoder(second.Body).Decode(&secondBody); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if secondBody.IsNewAccount {
		t.Fatal("expected the second login for the same identity to report is_new_account false")
	}
}

func TestHandlers_AppleLogin_InvalidToken_Returns401(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	resp := postJSON(t, server.URL+"/auth/apple", map[string]string{"identity_token": "not-a-real-token"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestHandlers_AppleLogin_MissingToken_Returns400(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	resp := postJSON(t, server.URL+"/auth/apple", map[string]string{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestHandlers_FullLoginRefreshLogoutFlow(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	token := signTestAppleToken(t, priv, kid, "apple-subject-flow")

	loginResp := postJSON(t, server.URL+"/auth/apple", map[string]string{"identity_token": token})
	var loginBody struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(loginResp.Body).Decode(&loginBody)
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected status %d, got %d", http.StatusOK, loginResp.StatusCode)
	}

	refreshResp := postJSON(t, server.URL+"/auth/refresh", map[string]string{"refresh_token": loginBody.RefreshToken})
	var refreshBody struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(refreshResp.Body).Decode(&refreshBody)
	refreshResp.Body.Close()
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("refresh: expected status %d, got %d", http.StatusOK, refreshResp.StatusCode)
	}

	reuseResp := postJSON(t, server.URL+"/auth/refresh", map[string]string{"refresh_token": loginBody.RefreshToken})
	reuseResp.Body.Close()
	if reuseResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reuse: expected status %d, got %d", http.StatusUnauthorized, reuseResp.StatusCode)
	}

	logoutResp := postJSON(t, server.URL+"/auth/logout", map[string]string{"refresh_token": refreshBody.RefreshToken})
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected status %d, got %d", http.StatusOK, logoutResp.StatusCode)
	}
}

// MARK: - Profile

func TestHandlers_GetOwnProfile_Success_FreshAccountHasEmptyFields(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	accessToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-profile-1")

	resp := authenticatedRequest(t, http.MethodGet, server.URL+"/profile/me", accessToken, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var body struct {
		DisplayName string `json:"display_name"`
		AvatarID    string `json:"avatar_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.DisplayName != "" {
		t.Errorf("expected a fresh account's display_name to be empty, got %q", body.DisplayName)
	}
	if body.AvatarID != "" {
		t.Errorf("expected a fresh account's avatar_id to be empty (Avatar Selection is ), got %q", body.AvatarID)
	}
}

func TestHandlers_GetOwnProfile_NoToken_Returns401(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/profile/me")
	if err != nil {
		t.Fatalf("GET /profile/me: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestHandlers_GetOwnProfile_InvalidToken_Returns401(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	resp := authenticatedRequest(t, http.MethodGet, server.URL+"/profile/me", "not-a-real-access-token", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestHandlers_UpdateOwnProfile_Success_PersistsDisplayName(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	accessToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-profile-2")

	updateResp := authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", accessToken, map[string]string{"display_name": "Ada"})
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /profile/me: expected status %d, got %d", http.StatusOK, updateResp.StatusCode)
	}

	var updateBody struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(updateResp.Body).Decode(&updateBody); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateBody.DisplayName != "Ada" {
		t.Fatalf("expected the update response to reflect the new display_name, got %q", updateBody.DisplayName)
	}

	getResp := authenticatedRequest(t, http.MethodGet, server.URL+"/profile/me", accessToken, nil)
	defer getResp.Body.Close()
	var getBody struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&getBody); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getBody.DisplayName != "Ada" {
		t.Fatalf("expected persisted display_name %q, got %q", "Ada", getBody.DisplayName)
	}
}

// MARK: - Avatar

func TestHandlers_UpdateOwnProfile_ValidAvatar_Persists(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	accessToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-avatar-1")

	updateResp := authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", accessToken, map[string]string{"avatar_id": "avatar_3"})
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /profile/me: expected status %d, got %d", http.StatusOK, updateResp.StatusCode)
	}

	var body struct {
		AvatarID string `json:"avatar_id"`
	}
	if err := json.NewDecoder(updateResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.AvatarID != "avatar_3" {
		t.Fatalf("expected avatar_id %q, got %q", "avatar_3", body.AvatarID)
	}
}

func TestHandlers_UpdateOwnProfile_InvalidAvatar_Returns400(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	accessToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-avatar-2")

	resp := authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", accessToken, map[string]string{"avatar_id": "not-a-real-avatar"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestHandlers_UpdateOwnProfile_AvatarOnly_DoesNotWipeDisplayName(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	accessToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-avatar-3")

	setNameResp := authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", accessToken, map[string]string{"display_name": "Ada"})
	setNameResp.Body.Close()

	avatarOnlyResp := authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", accessToken, map[string]string{"avatar_id": "avatar_2"})
	defer avatarOnlyResp.Body.Close()

	var body struct {
		DisplayName string `json:"display_name"`
		AvatarID    string `json:"avatar_id"`
	}
	if err := json.NewDecoder(avatarOnlyResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.DisplayName != "Ada" {
		t.Fatalf("expected display_name to remain %q after an avatar-only update, got %q", "Ada", body.DisplayName)
	}
	if body.AvatarID != "avatar_2" {
		t.Fatalf("expected avatar_id %q, got %q", "avatar_2", body.AvatarID)
	}
}

func TestHandlers_UpdateOwnProfile_EmptyBody_Returns400(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	accessToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-avatar-4")

	resp := authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", accessToken, map[string]string{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestHandlers_GetProfile_OtherAccount_SeesTheAvatarAndNotTheDisplayName(t *testing.T) {
	server, priv, kid, accessTokens := setupTestServer(t)

	ownerToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-owner")
	visitorToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-visitor")

	setResp := authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", ownerToken,
		map[string]string{"display_name": "Museum Owner", "avatar_id": "avatar_3"})
	setResp.Body.Close()

	ownerClaims, err := accessTokens.Verify(ownerToken)
	if err != nil {
		t.Fatalf("verify owner access token: %v", err)
	}

	resp := authenticatedRequest(t, http.MethodGet, server.URL+"/profile/"+string(ownerClaims.AccountID), visitorToken, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if string(body["avatar_id"]) != `"avatar_3"` {
		t.Fatalf("the visitor must see the owner's Avatar, got %s", body["avatar_id"])
	}
	if _, leaked := body["display_name"]; leaked {
		t.Fatalf("another account's display_name must be withheld (no key at all), got %s", body["display_name"])
	}
	if len(body) != 1 {
		t.Fatalf("the public profile is exactly the Avatar, got %v", body)
	}

	own := authenticatedRequest(t, http.MethodGet, server.URL+"/profile/"+string(ownerClaims.AccountID), ownerToken, nil)
	defer own.Body.Close()
	var ownBody struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(own.Body).Decode(&ownBody); err != nil || ownBody.DisplayName != "Museum Owner" {
		t.Fatalf("the owner's own by-id read must carry the display name, got %q (%v)", ownBody.DisplayName, err)
	}
}

func TestHandlers_GetProfile_NoToken_Returns401(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	ownerToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-owner-2")
	setNameResp := authenticatedRequest(t, http.MethodPatch, server.URL+"/profile/me", ownerToken, map[string]string{"display_name": "X"})
	setNameResp.Body.Close()

	resp, err := http.Get(server.URL + "/profile/some-account-id")
	if err != nil {
		t.Fatalf("GET /profile/{id}: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestHandlers_GetProfile_UnknownAccount_Returns404(t *testing.T) {
	server, priv, kid, _ := setupTestServer(t)
	callerToken := loginAndGetAccessToken(t, server, priv, kid, "apple-subject-caller")

	resp := authenticatedRequest(t, http.MethodGet, server.URL+"/profile/does-not-exist", callerToken, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.StatusCode)
	}
}
