package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestNonSelfAndPublicProfileSurfacesAreAvatarOnly(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	stranger := s.strangerToken()

	const name = "Grace Hopper"
	if resp, raw := s.do(http.MethodPatch, "/profile/me",
		map[string]string{"display_name": name, "avatar_id": "avatar_4"}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("set profile: %d %s", resp.StatusCode, raw)
	}

	other := s.get("/profile/"+s.accountID, stranger)
	if other.status != http.StatusOK {
		t.Fatalf("a non-self profile read must succeed: %d %s", other.status, other.body)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(other.body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || string(payload["avatar_id"]) != `"avatar_4"` {
		t.Fatalf("a non-self profile is exactly {avatar_id}, got %s", other.body)
	}
	if _, present := payload["display_name"]; present {
		t.Fatalf("display_name must be absent, not empty: %s", other.body)
	}

	for label, r := range map[string]reply{
		"/profile/me":    s.get("/profile/me", s.token),
		"own read by id": s.get("/profile/"+s.accountID, s.token),
	} {
		if !strings.Contains(r.body, name) {
			t.Errorf("%s must still carry the owner's own display name: %s", label, r.body)
		}
	}

	for label, r := range map[string]reply{
		"pre-auth preview":        s.get("/share-links/"+f.code, ""),
		"museum landing page":     s.get("/m/"+f.code, ""),
		"collection landing page": s.get("/c/"+f.collectionCode, ""),
		"visitor museum":          s.get("/share-links/"+f.code+"/museum", stranger),
		"visitor room":            s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom, stranger),
		"visitor collection room": s.visitCollectionRoom(f.collectionCode, stranger),
	} {
		if r.status != http.StatusOK {
			t.Fatalf("%s must be readable for this assertion to mean anything: %d %s", label, r.status, r.body)
		}
		if strings.Contains(r.body, name) || strings.Contains(r.body, "display_name") {
			t.Errorf("%s exposes the owner's display name: %s", label, r.body)
		}
	}

	preview := s.get("/share-links/"+f.code, "")
	if !strings.Contains(preview.body, `"avatar_id":"avatar_4"`) {
		t.Fatalf("the preview must still carry the owner's Avatar: %s", preview.body)
	}
}
