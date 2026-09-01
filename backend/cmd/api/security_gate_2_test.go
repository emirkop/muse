package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type reply struct {
	status int
	body   string
}

func (s *stack) get(path, token string) reply {
	s.t.Helper()
	resp, raw := s.do(http.MethodGet, path, nil, token)
	return reply{status: resp.StatusCode, body: string(raw)}
}

func mustBeIndistinguishable(t *testing.T, replies map[string]reply) {
	t.Helper()
	var referenceName string
	var reference reply
	for name, r := range replies {
		referenceName, reference = name, r
		break
	}
	for name, r := range replies {
		if r.status != reference.status || r.body != reference.body {
			t.Errorf("distinguishable: %q → %d %s  vs  %q → %d %s",
				name, r.status, r.body, referenceName, reference.status, reference.body)
		}
	}
}

func (s *stack) randomID() string {
	s.t.Helper()
	var id string
	if err := s.pool.Pool().QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		s.t.Fatal(err)
	}
	return id
}

func (s *stack) ownMuseumID(token string) string {
	s.t.Helper()
	r := s.get("/museum/me", token)
	if r.status != http.StatusOK {
		s.t.Fatalf("GET /museum/me: %d %s", r.status, r.body)
	}
	var body struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(r.body), &body)
	return body.ID
}

func (s *stack) setMuseumPrivacy(token, privacy string) {
	s.t.Helper()
	if resp, raw := s.do(http.MethodPatch, "/museum/me/privacy", map[string]string{"privacy": privacy}, token); resp.StatusCode != http.StatusOK {
		s.t.Fatalf("set museum privacy: %d %s", resp.StatusCode, raw)
	}
}

func (s *stack) setRoomPrivacy(token, roomID, privacy string) {
	s.t.Helper()
	if resp, raw := s.do(http.MethodPatch, "/museum/me/rooms/"+roomID, map[string]string{"privacy": privacy}, token); resp.StatusCode != http.StatusOK {
		s.t.Fatalf("set room privacy: %d %s", resp.StatusCode, raw)
	}
}

type gate2Fixture struct {
	museumID, publicRoom, privateRoom  string
	stranger, strangerWithMuseum       string
	strangerMuseumID                   string
	nonexistentMuseum, nonexistentRoom string
}

func newGate2Fixture(t *testing.T, s *stack) gate2Fixture {
	t.Helper()
	publicRoom := s.createRoom()
	resp, raw := s.do(http.MethodPost, "/museum/me/rooms", map[string]string{"name": "Private Study", "variant_id": "style_modern_variant_Hall"}, s.token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second room: %d %s", resp.StatusCode, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &created)

	f := gate2Fixture{
		museumID:    s.ownMuseumID(s.token),
		publicRoom:  publicRoom,
		privateRoom: created.ID,
	}
	s.setRoomPrivacy(s.token, f.publicRoom, "public")

	f.stranger = s.strangerToken()
	f.strangerWithMuseum = s.strangerToken()
	if resp, raw := s.do(http.MethodPost, "/museum", map[string]string{"style_id": "style_modern"}, f.strangerWithMuseum); resp.StatusCode != http.StatusCreated {
		t.Fatalf("stranger museum: %d %s", resp.StatusCode, raw)
	}
	f.strangerMuseumID = s.ownMuseumID(f.strangerWithMuseum)
	s.setMuseumPrivacy(f.strangerWithMuseum, "public")

	f.nonexistentMuseum = s.randomID()
	f.nonexistentRoom = s.randomID()
	return f
}

func TestSecurityGate2_PrivateMuseumIsIndistinguishableFromNonexistent(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)

	for name, token := range map[string]string{"stranger without museum": f.stranger, "stranger with own museum": f.strangerWithMuseum} {
		t.Run(name, func(t *testing.T) {
			replies := map[string]reply{
				"nonexistent museum":                 s.get("/museums/"+f.nonexistentMuseum, token),
				"existing private museum":            s.get("/museums/"+f.museumID, token),
				"malformed museum id":                s.get("/museums/not-an-id", token),
				"public room inside private museum":  s.get("/museums/"+f.museumID+"/rooms/"+f.publicRoom, token),
				"private room inside private museum": s.get("/museums/"+f.museumID+"/rooms/"+f.privateRoom, token),
				"nonexistent room inside it":         s.get("/museums/"+f.museumID+"/rooms/"+f.nonexistentRoom, token),
				"real room under nonexistent museum": s.get("/museums/"+f.nonexistentMuseum+"/rooms/"+f.publicRoom, token),
			}
			mustBeIndistinguishable(t, replies)
			if r := replies["nonexistent museum"]; r.status != http.StatusNotFound || !strings.Contains(r.body, "not found") {
				t.Fatalf("the shared refusal must be a plain 404, got %d %s", r.status, r.body)
			}
		})
	}
}

func TestSecurityGate2_BareMuseumIDIsNotVisitorAuthority_EvenWhenPublic(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	s.setMuseumPrivacy(s.token, "public")

	for name, token := range map[string]string{"stranger without museum": f.stranger, "stranger with own museum": f.strangerWithMuseum} {
		t.Run(name, func(t *testing.T) {
			replies := map[string]reply{
				"nonexistent museum":                s.get("/museums/"+f.nonexistentMuseum, token),
				"existing PUBLIC museum":            s.get("/museums/"+f.museumID, token),
				"malformed museum id":               s.get("/museums/not-an-id", token),
				"public room inside public museum":  s.get("/museums/"+f.museumID+"/rooms/"+f.publicRoom, token),
				"private room inside public museum": s.get("/museums/"+f.museumID+"/rooms/"+f.privateRoom, token),
				"nonexistent room":                  s.get("/museums/"+f.museumID+"/rooms/"+f.nonexistentRoom, token),
				"malformed room id":                 s.get("/museums/"+f.museumID+"/rooms/not-an-id", token),
				"public room under WRONG museum":    s.get("/museums/"+f.strangerMuseumID+"/rooms/"+f.publicRoom, token),
			}
			mustBeIndistinguishable(t, replies)
			if r := replies["existing PUBLIC museum"]; r.status != http.StatusNotFound {
				t.Fatalf("a bare Museum id must never resolve for a non-owner, got %d %s", r.status, r.body)
			}
		})
	}
}

func TestSecurityGate2_OwnerAlwaysSeesTheirOwnPrivateContent(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)

	museum := s.get("/museums/"+f.museumID, s.token)
	if museum.status != http.StatusOK {
		t.Fatalf("owner must see their Private Museum: %d %s", museum.status, museum.body)
	}
	var decoded struct {
		Rooms []map[string]any `json:"rooms"`
	}
	_ = json.Unmarshal([]byte(museum.body), &decoded)
	if len(decoded.Rooms) != 2 {
		t.Fatalf("owner must see both Rooms, got %d", len(decoded.Rooms))
	}
	if r := s.get("/museums/"+f.museumID+"/rooms/"+f.privateRoom, s.token); r.status != http.StatusOK {
		t.Fatalf("owner must see their Private Room: %d %s", r.status, r.body)
	}
	if r := s.get("/museum/me/rooms/"+f.privateRoom, s.token); r.status != http.StatusOK || !strings.Contains(r.body, `"privacy":"private"`) {
		t.Fatalf("the owner's own view must still carry privacy: %d %s", r.status, r.body)
	}
	if strings.Contains(museum.body, `"privacy"`) {
		t.Fatalf("the shared read boundary carries no privacy field even for the owner: %s", museum.body)
	}
}

func TestSecurityGate2_ForeignRoomUnderMuseumMeIsIndistinguishableFromNonexistent(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	before := s.snapshotOwnerState()

	for name, token := range map[string]string{"stranger without museum": f.stranger, "stranger with own museum": f.strangerWithMuseum} {
		t.Run(name, func(t *testing.T) {
			nonexistent := s.get("/museum/me/rooms/"+f.nonexistentRoom, token)
			foreign := s.get("/museum/me/rooms/"+f.publicRoom, token)
			foreignPrivate := s.get("/museum/me/rooms/"+f.privateRoom, token)
			mustBeIndistinguishable(t, map[string]reply{
				"nonexistent room": nonexistent, "foreign public room": foreign, "foreign private room": foreignPrivate,
			})
			if nonexistent.status != http.StatusNotFound {
				t.Fatalf("expected 404, got %d", nonexistent.status)
			}

			for _, attempt := range []struct {
				method, path string
				body         any
			}{
				{http.MethodPatch, "/museum/me/rooms/" + f.publicRoom, map[string]string{"privacy": "private"}},
				{http.MethodDelete, "/museum/me/rooms/" + f.publicRoom, nil},
				{http.MethodPatch, "/museum/me/rooms/" + f.nonexistentRoom, map[string]string{"privacy": "private"}},
			} {
				resp, raw := s.do(attempt.method, attempt.path, attempt.body, token)
				if resp.StatusCode != http.StatusNotFound || string(raw) != nonexistent.body {
					t.Errorf("%s %s → %d %s; want the same 404 as a nonexistent Room (%s)", attempt.method, attempt.path, resp.StatusCode, raw, nonexistent.body)
				}
			}
		})
	}
	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("nothing of the owner's may change:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestSecurityGate2_PrivacyMutationIsOwnerOnly(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	before := s.snapshotOwnerState()

	if resp, _ := s.do(http.MethodPatch, "/museum/me/privacy", map[string]string{"privacy": "public"}, f.stranger); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	s.setMuseumPrivacy(f.strangerWithMuseum, "private")
	s.setMuseumPrivacy(f.strangerWithMuseum, "public")
	if r := s.get("/museums/"+f.museumID, f.stranger); r.status != http.StatusNotFound {
		t.Fatal("the owner's Museum must still be Private")
	}
	for _, token := range []string{f.stranger, f.strangerWithMuseum} {
		if resp, _ := s.do(http.MethodPatch, "/museum/me/rooms/"+f.privateRoom, map[string]string{"privacy": "public"}, token); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	}
	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("the owner's privacy state must be untouched:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestSecurityGate2_PrivacyChangesTakeEffectOnTheNextRequest(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	code := s.ensureShareLink(s.token).code
	unknown := s.get("/share-links/ZZZZZZZZZZZZZZZZZZZZZZ/museum", f.stranger)

	if r := s.get("/share-links/"+code+"/museum", f.stranger); r != unknown {
		t.Fatalf("private to begin with: %+v vs %+v", r, unknown)
	}
	s.setMuseumPrivacy(s.token, "public")
	if r := s.get("/share-links/"+code+"/museum", f.stranger); r.status != http.StatusOK {
		t.Fatalf("visible on the next request after publishing, got %d %s", r.status, r.body)
	}
	if r := s.get("/share-links/"+code+"/rooms/"+f.publicRoom, f.stranger); r.status != http.StatusOK {
		t.Fatalf("the Public Room is visible, got %d", r.status)
	}

	s.setRoomPrivacy(s.token, f.publicRoom, "private")
	nonexistentRoom := s.get("/share-links/"+code+"/rooms/"+f.nonexistentRoom, f.stranger)
	if r := s.get("/share-links/"+code+"/rooms/"+f.publicRoom, f.stranger); r != nonexistentRoom {
		t.Fatalf("a Room made Private is indistinguishable from nonexistent on the next request: %+v vs %+v", r, nonexistentRoom)
	}
	museum := s.get("/share-links/"+code+"/museum", f.stranger)
	if strings.Contains(museum.body, f.publicRoom) {
		t.Fatal("the newly-Private Room must vanish from the listing")
	}

	s.setMuseumPrivacy(s.token, "private")
	if r := s.get("/share-links/"+code+"/museum", f.stranger); r != unknown {
		t.Fatalf("the Museum made Private is indistinguishable from an unknown link on the next request: %+v vs %+v", r, unknown)
	}
}

func TestSecurityGate2_RoomPrivacyPatchTouchesOnlyPrivacy(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	before := s.get("/museum/me/rooms/"+f.privateRoom, s.token)
	var b struct{ Name, VariantID string }
	_ = json.Unmarshal([]byte(before.body), &struct {
		Name      *string `json:"name"`
		VariantID *string `json:"variant_id"`
	}{&b.Name, &b.VariantID})

	s.setRoomPrivacy(s.token, f.privateRoom, "public")

	after := s.get("/museum/me/rooms/"+f.privateRoom, s.token)
	var a struct {
		Name      string `json:"name"`
		VariantID string `json:"variant_id"`
		Privacy   string `json:"privacy"`
	}
	_ = json.Unmarshal([]byte(after.body), &a)
	if a.Privacy != "public" {
		t.Fatalf("privacy must change, got %q", a.Privacy)
	}
	if a.Name != b.Name || a.VariantID != b.VariantID {
		t.Fatalf("name/variant must be untouched: before %s / %s, after %s / %s", b.Name, b.VariantID, a.Name, a.VariantID)
	}
	if resp, raw := s.do(http.MethodPatch, "/museum/me/rooms/"+f.privateRoom, map[string]string{}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("empty PATCH: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := s.do(http.MethodPatch, "/museum/me/rooms/"+f.privateRoom, map[string]string{"privacy": "unlisted"}, s.token); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid privacy value, got %d", resp.StatusCode)
	}
}

func TestSecurityGate2_UnauthenticatedReadsAreUniform(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	s.setMuseumPrivacy(s.token, "public")

	mustBeIndistinguishable(t, map[string]reply{
		"real public museum": s.get("/museums/"+f.museumID, ""),
		"nonexistent museum": s.get("/museums/"+f.nonexistentMuseum, ""),
		"real public room":   s.get("/museums/"+f.museumID+"/rooms/"+f.publicRoom, ""),
		"nonexistent room":   s.get("/museums/"+f.museumID+"/rooms/"+f.nonexistentRoom, ""),
	})
	if r := s.get("/museums/"+f.museumID, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", r.status)
	}
}
