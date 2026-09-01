package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOversizedJSONBodies_AreRefusedAndCreateNothing(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	before := s.snapshotOwnerState()

	padding := strings.Repeat("a", 2<<20)
	attempts := []struct {
		name, method, path string
		body               string
		token              string
	}{
		{"identity refresh", http.MethodPost, "/auth/refresh", `{"refresh_token":"` + padding + `"}`, ""},
		{"identity email login", http.MethodPost, "/auth/email/login", `{"email":"a@b.c","password":"` + padding + `"}`, ""},
		{"museum create room", http.MethodPost, "/museum/me/rooms", `{"name":"` + padding + `","variant_id":"style_modern_variant_Hall"}`, s.token},
		{"museum caption", http.MethodPut, "/museum/me/rooms/" + roomID + "/photo-order", `{"photo_asset_ids":["` + padding + `"]}`, s.token},
		{"collection create", http.MethodPost, "/collection-rooms", `{"name":"` + padding + `","category_id":"watches"}`, s.token},
		{"media upload declaration", http.MethodPost, "/media/photo-uploads", `{"client_upload_id":"` + padding + `"}`, s.token},
	}
	for _, at := range attempts {
		t.Run(at.name, func(t *testing.T) {
			req, err := http.NewRequest(at.method, s.server.URL+at.path, bytes.NewReader([]byte(at.body)))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			if at.token != "" {
				req.Header.Set("Authorization", "Bearer "+at.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				raw, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != http.StatusBadRequest {
					t.Fatalf("%s: oversized body → %d %s; want 400 (or a closed connection)", at.name, resp.StatusCode, raw)
				}
			}
		})
	}
	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("an oversized body created or changed something:\nbefore %+v\nafter  %+v", before, after)
	}
	if resp, raw := s.do(http.MethodPost, "/museum/me/rooms", map[string]string{"name": "Fits", "variant_id": "style_modern_variant_Hall"}, s.token); resp.StatusCode != http.StatusCreated {
		t.Fatalf("a normal body must still be accepted: %d %s", resp.StatusCode, raw)
	}
}

func TestShareLandingPages_CarryHardeningHeaders(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)

	for name, path := range map[string]string{
		"museum landing":      "/m/" + f.code,
		"museum dead link":    "/m/" + unknownCode,
		"collection landing":  "/c/" + f.collectionCode,
		"collection any code": "/c/" + unknownCode,
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := http.Get(s.server.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
				t.Fatalf("%s is not an HTML page: %q", path, resp.Header.Get("Content-Type"))
			}
			want := map[string]string{
				"X-Content-Type-Options":  "nosniff",
				"Referrer-Policy":         "no-referrer",
				"X-Frame-Options":         "DENY",
				"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
			}
			for header, value := range want {
				if got := resp.Header.Get(header); got != value {
					t.Errorf("%s: %s = %q, want %q", path, header, got, value)
				}
			}
			if strings.Contains(string(body), "<script") || strings.Contains(string(body), "src=") {
				t.Fatalf("%s contains script or an external reference the CSP would block: %s", path, body)
			}
		})
	}
}

func TestProfileByID_ExposesOnlyTheAvatarToOtherAccounts(t *testing.T) {
	s := newStack(t)
	if resp, raw := s.do(http.MethodPatch, "/profile/me", map[string]string{"display_name": "Ada Lovelace", "avatar_id": "avatar_2"}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("set owner profile: %d %s", resp.StatusCode, raw)
	}
	stranger := s.strangerToken()

	self := s.get("/profile/"+s.accountID, s.token)
	if self.status != http.StatusOK || !strings.Contains(self.body, `"display_name":"Ada Lovelace"`) {
		t.Fatalf("owner reading their own profile by id: %d %s", self.status, self.body)
	}

	other := s.get("/profile/"+s.accountID, stranger)
	if other.status != http.StatusOK {
		t.Fatalf("stranger reading a profile by id: %d %s", other.status, other.body)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(other.body), &payload); err != nil {
		t.Fatal(err)
	}
	if _, leaked := payload["display_name"]; leaked {
		t.Fatalf("another account's display name is exposed: %s", other.body)
	}
	if string(payload["avatar_id"]) != `"avatar_2"` {
		t.Fatalf("avatar must still be visible to others (01 §4.2): %s", other.body)
	}
	if len(payload) != 1 {
		t.Fatalf("the public profile is exactly one field: %s", other.body)
	}
	if strings.Contains(other.body, "Ada") {
		t.Fatalf("display name text leaked through some other field: %s", other.body)
	}

	if strings.Contains(other.body, s.accountID) {
		t.Fatalf("the public profile must not echo the account id: %s", other.body)
	}
}
