package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type ownerIdentifiers struct {
	accountID    string
	displayName  string
	museumID     string
	privateRoom  string
	collectionID string
	assetID      string
	storageKey   string
}

func (s *stack) ownerIdentifiers(t *testing.T, f sweepFixture) ownerIdentifiers {
	t.Helper()
	var storageKey string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT storage_key FROM assets WHERE id = $1`, f.assetID).Scan(&storageKey); err != nil {
		t.Fatal(err)
	}
	return ownerIdentifiers{
		accountID:    s.accountID,
		displayName:  "Ada Privacy Lovelace",
		museumID:     f.museumID,
		privateRoom:  f.privateRoom,
		collectionID: f.collectionRoomID,
		assetID:      f.assetID,
		storageKey:   storageKey,
	}
}

func TestPrivateContentIsAbsentFromVisitorPayloads_NotMerelyRefused(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	visitor := s.strangerToken()

	museum := s.get("/share-links/"+f.code+"/museum", visitor)
	if museum.status != http.StatusOK {
		t.Fatalf("visitor Museum read: %d %s", museum.status, museum.body)
	}

	for _, forbidden := range []string{f.privateRoom, "Private Study", "hidden", "private", "Private"} {
		if strings.Contains(museum.body, forbidden) {
			t.Errorf("the visitor's Museum payload carries %q: %s", forbidden, museum.body)
		}
	}
	var payload struct {
		MuseumID string `json:"museum_id"`
		StyleID  string `json:"style_id"`
		Rooms    []struct {
			ID string `json:"id"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal([]byte(museum.body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Rooms) != 1 || payload.Rooms[0].ID != f.publicRoom {
		t.Fatalf("exactly the one Public Room must be listed, got %+v", payload.Rooms)
	}

	privateRoom := s.get("/share-links/"+f.code+"/rooms/"+f.privateRoom, visitor)
	nonexistent := s.get("/share-links/"+f.code+"/rooms/"+bogusUUID, visitor)
	if privateRoom != nonexistent {
		t.Fatalf("a Private Room (%d %s) must be indistinguishable from a nonexistent one (%d %s)",
			privateRoom.status, privateRoom.body, nonexistent.status, nonexistent.body)
	}
	privateTickets := s.get("/share-links/"+f.code+"/rooms/"+f.privateRoom+"/photo-urls", visitor)
	nonexistentTickets := s.get("/share-links/"+f.code+"/rooms/"+bogusUUID+"/photo-urls", visitor)
	if privateTickets != nonexistentTickets {
		t.Fatalf("Private Room tickets (%d %s) vs nonexistent (%d %s)",
			privateTickets.status, privateTickets.body, nonexistentTickets.status, nonexistentTickets.body)
	}

	if strings.Contains(museum.body, f.collectionRoomID) {
		t.Fatalf("a Museum link's payload names a Collection Room: %s", museum.body)
	}
}

func TestMuseumPrivacyChange_RemovesEverythingFromTheVisitorsView(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	visitor := s.strangerToken()

	if r := s.get("/share-links/"+f.code+"/museum", visitor); r.status != http.StatusOK {
		t.Fatalf("precondition: the visitor can read a Public Museum, got %d", r.status)
	}
	s.setMuseumPrivacy(s.token, "private")

	after := s.get("/share-links/"+f.code+"/museum", visitor)
	unknown := s.get("/share-links/"+unknownCode+"/museum", visitor)
	if after != unknown {
		t.Fatalf("a Private Museum (%d %s) must read as an unknown link (%d %s)",
			after.status, after.body, unknown.status, unknown.body)
	}
	room := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom, visitor)
	if room.status != http.StatusNotFound {
		t.Fatalf("a Public Room inside a Private Museum must not be readable: %d %s", room.status, room.body)
	}
	if r := s.get("/museum/me/rooms/"+f.publicRoom, s.token); r.status != http.StatusOK {
		t.Fatalf("the owner must keep reading their own content: %d %s", r.status, r.body)
	}
}

func TestNoVisitorReachableSurfaceCarriesOwnerIdentityOrCredentials(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	visitor := s.strangerToken()

	if resp, raw := s.do(http.MethodPatch, "/profile/me",
		map[string]string{"display_name": "Ada Privacy Lovelace", "avatar_id": "avatar_2"}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("set owner profile: %d %s", resp.StatusCode, raw)
	}
	ids := s.ownerIdentifiers(t, f)

	surfaces := map[string]reply{
		"pre-auth museum preview":  s.get("/share-links/"+f.code, ""),
		"museum landing page":      s.get("/m/"+f.code, ""),
		"visitor museum":           s.get("/share-links/"+f.code+"/museum", visitor),
		"visitor room":             s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom, visitor),
		"visitor collection room":  s.visitCollectionRoom(f.collectionCode, visitor),
		"collection landing page":  s.get("/c/"+f.collectionCode, ""),
		"owner profile as visitor": s.get("/profile/"+ids.accountID, visitor),
	}

	forbidden := map[string]string{
		"the owner's display name": ids.displayName,
		"an email address":         "@privaterelay.appleid.com",
		"the owner's account id":   ids.accountID,
		"the Private Room":         ids.privateRoom,
		"a storage key":            ids.storageKey,
		"a refresh token":          s.token,
	}
	for name, r := range surfaces {
		if r.status != http.StatusOK {
			t.Fatalf("%s must be readable for this test to mean anything: %d %s", name, r.status, r.body)
		}
		for label, needle := range forbidden {
			if needle == "" {
				continue
			}
			if strings.Contains(r.body, needle) {
				t.Errorf("%s leaks %s (%q): %s", name, label, needle, r.body)
			}
		}
	}
}

func TestPhotoTicketExposesTheAccountIDAndNothingElse_RecordedDebt(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	visitor := s.strangerToken()

	if resp, raw := s.do(http.MethodPatch, "/profile/me",
		map[string]string{"display_name": "Ada Privacy Lovelace"}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("set owner profile: %d %s", resp.StatusCode, raw)
	}
	ids := s.ownerIdentifiers(t, f)

	tickets := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", visitor)
	if tickets.status != http.StatusOK {
		t.Fatalf("visitor tickets: %d %s", tickets.status, tickets.body)
	}

	if !strings.Contains(tickets.body, ids.accountID) {
		t.Log("note: the ticket URL no longer embeds the account id — if that was deliberate, " +
			"update debt item 3 and this test")
	}
	for label, needle := range map[string]string{
		"display name":  ids.displayName,
		"private room":  ids.privateRoom,
		"email":         "@privaterelay.appleid.com",
		"museum id":     ids.museumID,
		"collection id": ids.collectionID,
	} {
		if needle != "" && strings.Contains(tickets.body, needle) {
			t.Errorf("a photo ticket leaks the %s: %s", label, tickets.body)
		}
	}
	var payload struct {
		Tickets []map[string]json.RawMessage `json:"tickets"`
	}
	if err := json.Unmarshal([]byte(tickets.body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Tickets) == 0 {
		t.Fatalf("expected at least one ticket: %s", tickets.body)
	}
	for _, ticket := range payload.Tickets {
		for key := range ticket {
			switch key {
			case "photo_asset_id", "url", "expires_at", "pixel_width", "pixel_height":
			default:
				t.Errorf("unexpected field %q in a photo ticket — Gate #3 fixed this payload's shape", key)
			}
		}
	}
}

func TestDeactivatedAccountProfileIsUnreadableEverywhere_ContentIsPD012(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	visitor := s.strangerToken()

	if r := s.get("/share-links/"+f.code, ""); r.status != http.StatusOK {
		t.Fatalf("precondition: preview readable, got %d %s", r.status, r.body)
	}
	if r := s.get("/profile/"+s.accountID, visitor); r.status != http.StatusOK {
		t.Fatalf("precondition: profile readable, got %d %s", r.status, r.body)
	}

	if _, err := s.pool.Pool().Exec(context.Background(),
		`UPDATE accounts SET deleted_at = now() WHERE id = $1`, s.accountID); err != nil {
		t.Fatal(err)
	}

	if r := s.get("/profile/"+s.accountID, visitor); r.status != http.StatusNotFound {
		t.Errorf("a deactivated account's profile must not be readable by id: %d %s", r.status, r.body)
	}
	preview := s.get("/share-links/"+f.code, "")
	unknown := s.get("/share-links/"+unknownCode, "")
	if preview != unknown {
		t.Errorf("a deactivated owner's preview (%d %s) must read as an unknown link (%d %s)",
			preview.status, preview.body, unknown.status, unknown.body)
	}
	if strings.Contains(preview.body, "avatar_id") {
		t.Errorf("the preview still carries the deactivated owner's Avatar: %s", preview.body)
	}

	content := s.get("/share-links/"+f.code+"/museum", visitor)
	t.Logf("DOCUMENTED for (open, unreachable today — no deletion flow exists): a deactivated "+
		"owner's Public Museum content through a live share link answers %d. fixed the PROFILE "+
		"inconsistency only and did not decide deletion semantics.", content.status)
}
