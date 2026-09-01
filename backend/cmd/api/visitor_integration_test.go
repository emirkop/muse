package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type visitorFixture struct {
	gate2Fixture
	code         string
	publicAsset  string
	privateAsset string
	ownerAccount string
}

func newVisitorFixture(t *testing.T, s *stack) visitorFixture {
	t.Helper()
	f := newGate2Fixture(t, s)
	s.setMuseumPrivacy(s.token, "public")

	publicPhoto := s.uploaded(newPhoto(t, 640, 480, "visitor-public-photo"))
	if resp, _, body := s.assign(f.publicRoom, []string{publicPhoto.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign public room photo: %d %v", resp.StatusCode, body)
	}
	privatePhoto := s.uploaded(newPhoto(t, 640, 480, "visitor-private-photo"))
	if resp, _, body := s.assign(f.privateRoom, []string{privatePhoto.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign private room photo: %d %v", resp.StatusCode, body)
	}

	return visitorFixture{
		gate2Fixture: f,
		code:         s.ensureShareLink(s.token).code,
		publicAsset:  publicPhoto.asset,
		privateAsset: privatePhoto.asset,
		ownerAccount: s.accountID,
	}
}

func TestVisitorMuseum_ContainsOnlyAuthorizedRooms(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	for name, token := range map[string]string{"stranger without museum": f.stranger, "stranger with own museum": f.strangerWithMuseum} {
		t.Run(name, func(t *testing.T) {
			r := s.get("/share-links/"+f.code+"/museum", token)
			if r.status != http.StatusOK {
				t.Fatalf("visitor read: %d %s", r.status, r.body)
			}
			var decoded struct {
				MuseumID string `json:"museum_id"`
				Rooms    []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"rooms"`
			}
			if err := json.Unmarshal([]byte(r.body), &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.Rooms) != 1 || decoded.Rooms[0].ID != f.publicRoom {
				t.Fatalf("exactly the Public Room, got %s", r.body)
			}
			for _, forbidden := range []string{f.privateRoom, "Private Study", `"privacy"`, "hidden", "locked"} {
				if strings.Contains(r.body, forbidden) {
					t.Fatalf("the payload must not carry %q: %s", forbidden, r.body)
				}
			}
		})
	}
}

func TestVisitorRoom_AndItsPhotographs_AreReadable(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	room := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom, f.stranger)
	if room.status != http.StatusOK {
		t.Fatalf("visitor room: %d %s", room.status, room.body)
	}
	if !strings.Contains(room.body, f.publicAsset) || strings.Contains(room.body, `"privacy"`) {
		t.Fatalf("the Room's content, without privacy: %s", room.body)
	}

	tickets := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger)
	if tickets.status != http.StatusOK {
		t.Fatalf("visitor tickets: %d %s", tickets.status, tickets.body)
	}
	var decoded struct {
		Tickets []struct {
			PhotoAssetID string `json:"photo_asset_id"`
			URL          string `json:"url"`
			PixelWidth   int    `json:"pixel_width"`
		} `json:"tickets"`
	}
	if err := json.Unmarshal([]byte(tickets.body), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Tickets) != 1 || decoded.Tickets[0].PhotoAssetID != f.publicAsset {
		t.Fatalf("one ticket for the owner's photograph, got %s", tickets.body)
	}
	if decoded.Tickets[0].PixelWidth != 640 {
		t.Fatalf("stored dimensions must come through: %s", tickets.body)
	}
	resp, err := testGet(decoded.Tickets[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the visitor's ticket must redeem, got %d", resp.StatusCode)
	}
	rawResp, _ := s.do(http.MethodGet, "/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", nil, f.stranger)
	if rawResp.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("tickets must not be cacheable: %q", rawResp.Header.Get("Cache-Control"))
	}
}

func TestPrivateRoom_IsUnreachableThroughEveryVisitorRoute(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	mustBeIndistinguishable(t, map[string]reply{
		"private room content": s.get("/share-links/"+f.code+"/rooms/"+f.privateRoom, f.stranger),
		"nonexistent room":     s.get("/share-links/"+f.code+"/rooms/"+f.nonexistentRoom, f.stranger),
		"malformed room":       s.get("/share-links/"+f.code+"/rooms/not-an-id", f.stranger),
		"foreign room":         s.get("/share-links/"+f.code+"/rooms/"+f.strangerMuseumID, f.stranger),
	})
	mustBeIndistinguishable(t, map[string]reply{
		"private room tickets":     s.get("/share-links/"+f.code+"/rooms/"+f.privateRoom+"/photo-urls", f.stranger),
		"nonexistent room tickets": s.get("/share-links/"+f.code+"/rooms/"+f.nonexistentRoom+"/photo-urls", f.stranger),
		"malformed room tickets":   s.get("/share-links/"+f.code+"/rooms/not-an-id/photo-urls", f.stranger),
	})
	if r := s.get("/share-links/"+f.code+"/rooms/"+f.privateRoom+"/photo-urls", f.stranger); r.status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", r.status, r.body)
	}
	for _, path := range []string{
		"/share-links/" + f.code + "/museum",
		"/share-links/" + f.code + "/rooms/" + f.publicRoom,
		"/share-links/" + f.code + "/rooms/" + f.publicRoom + "/photo-urls",
	} {
		if body := s.get(path, f.stranger).body; strings.Contains(body, f.privateAsset) {
			t.Fatalf("%s leaks the Private Room's photograph: %s", path, body)
		}
	}
}

func TestMuseumPrivate_BlocksTheEntireVisitorExperience(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)
	s.setMuseumPrivacy(s.token, "private")

	unknown := s.get("/share-links/"+unknownCode+"/museum", f.stranger)
	mustBeIndistinguishable(t, map[string]reply{
		"museum":  s.get("/share-links/"+f.code+"/museum", f.stranger),
		"room":    s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom, f.stranger),
		"tickets": s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger),
		"unknown": unknown,
	})
	s.setMuseumPrivacy(s.token, "public")
	if r := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger); r.status != http.StatusOK {
		t.Fatalf("Public again must restore delivery on the next request: %d %s", r.status, r.body)
	}
}

func TestRegeneratingTheLink_EndsPhotographDelivery(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)
	if r := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger); r.status != http.StatusOK {
		t.Fatalf("live before: %d %s", r.status, r.body)
	}

	fresh := s.regenerateShareLink(s.token)

	mustBeIndistinguishable(t, map[string]reply{
		"old link tickets": s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger),
		"unknown link":     s.get("/share-links/"+unknownCode+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger),
	})
	if r := s.get("/share-links/"+fresh.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger); r.status != http.StatusOK {
		t.Fatalf("the new link delivers: %d %s", r.status, r.body)
	}
}

func TestVisitorHasNoMutationPath_EvenHoldingALiveLink(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)
	before := s.snapshotOwnerState()

	if r := s.get("/share-links/"+f.code+"/museum", f.stranger); r.status != http.StatusOK {
		t.Fatalf("the visit must be live: %d %s", r.status, r.body)
	}

	ownersResources := []struct {
		name, method, path string
		body               any
	}{
		{"rename the owner's Room", http.MethodPatch, "/museum/me/rooms/" + f.publicRoom, map[string]string{"name": "Hijacked"}},
		{"make the owner's Private Room public", http.MethodPatch, "/museum/me/rooms/" + f.privateRoom, map[string]string{"privacy": "public"}},
		{"delete the owner's Room", http.MethodDelete, "/museum/me/rooms/" + f.publicRoom, nil},
		{"reorder the owner's photographs", http.MethodPut, "/museum/me/rooms/" + f.publicRoom + "/photo-order", map[string]any{"photo_asset_ids": []string{f.publicAsset}}},
		{"caption the owner's photograph", http.MethodPut, "/museum/me/rooms/" + f.publicRoom + "/photos/" + f.publicAsset + "/caption", map[string]string{"caption": "mine now"}},
		{"delete the owner's photograph", http.MethodDelete, "/museum/me/rooms/" + f.publicRoom + "/photos/" + f.publicAsset, nil},
		{"replace the owner's photograph", http.MethodPost, "/museum/me/rooms/" + f.publicRoom + "/photos/" + f.publicAsset + "/replacement", map[string]string{"asset_id": f.privateAsset}},
		{"add a sculpture to the owner's Room", http.MethodPost, "/museum/me/rooms/" + f.publicRoom + "/sculptures", map[string]string{"catalog_id": "sculpture_x"}},
		{"remove a sculpture from the owner's Room", http.MethodDelete, "/museum/me/rooms/" + f.publicRoom + "/sculptures/0", nil},
		{"assign photographs into the owner's Room", http.MethodPost, "/museum/me/rooms/" + f.publicRoom + "/photos", map[string]any{"asset_ids": []string{f.publicAsset}}},
		{"take the owner's photo tickets", http.MethodGet, "/museum/me/rooms/" + f.publicRoom + "/photo-urls", nil},
		{"read the owner's Room directly", http.MethodGet, "/museum/me/rooms/" + f.publicRoom, nil},
		{"read the owner's Museum by id", http.MethodGet, "/museums/" + f.museumID, nil},
	}
	for _, token := range []string{f.stranger, f.strangerWithMuseum} {
		for _, at := range ownersResources {
			resp, raw := s.do(at.method, at.path, at.body, token)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s → %d %s; a visitor must be refused (404)", at.name, resp.StatusCode, raw)
			}
		}
	}

	for _, at := range []struct {
		name, method, path string
		body               any
	}{
		{"privacy", http.MethodPatch, "/museum/me/privacy", map[string]string{"privacy": "private"}},
		{"style", http.MethodPatch, "/museum/me/style", map[string]string{"style_id": "style_gothic"}},
		{"share link", http.MethodPost, "/museum/me/share-link/regenerate", nil},
	} {
		if resp, raw := s.do(at.method, at.path, at.body, f.stranger); resp.StatusCode != http.StatusNotFound {
			t.Errorf("self-scoped %s for a stranger with no Museum → %d %s; want 404", at.name, resp.StatusCode, raw)
		}
	}
	s.setMuseumPrivacy(f.strangerWithMuseum, "private")
	_ = s.regenerateShareLink(f.strangerWithMuseum)

	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("a visitor must change nothing of the owner's:\nbefore %+v\nafter  %+v", before, after)
	}
	if r := s.get("/share-links/"+f.code+"/museum", f.stranger); r.status != http.StatusOK {
		t.Fatalf("the owner's link is untouched by a visitor's own changes: %d %s", r.status, r.body)
	}
}

func TestOwnerFlowIsUnchanged(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	rooms := s.get("/museum/me/rooms", s.token)
	if !strings.Contains(rooms.body, f.publicRoom) || !strings.Contains(rooms.body, f.privateRoom) {
		t.Fatalf("the owner sees both Rooms: %s", rooms.body)
	}
	if !strings.Contains(rooms.body, `"privacy"`) {
		t.Fatalf("the owner's payload still carries privacy: %s", rooms.body)
	}
	if r := s.get("/museum/me/rooms/"+f.privateRoom+"/photo-urls", s.token); r.status != http.StatusOK || !strings.Contains(r.body, f.privateAsset) {
		t.Fatalf("the owner still gets tickets for their Private Room: %d %s", r.status, r.body)
	}
	viaLink := s.get("/share-links/"+f.code+"/museum", s.token)
	if strings.Contains(viaLink.body, f.privateRoom) {
		t.Fatalf("through the link, even the owner sees only Public Rooms: %s", viaLink.body)
	}
	if r := s.get("/share-links/"+f.code+"/rooms/"+f.privateRoom+"/photo-urls", s.token); r.status != http.StatusNotFound {
		t.Fatalf("the link never delivers a Private Room's bytes, even to the owner: %d %s", r.status, r.body)
	}
}
