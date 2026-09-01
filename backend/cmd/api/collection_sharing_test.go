package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type collectionLinkJSON struct {
	collectionRoomID, code, url string
}

func decodeCollectionLink(t *testing.T, raw []byte) collectionLinkJSON {
	t.Helper()
	var body struct {
		CollectionRoomID string `json:"collection_room_id"`
		Code             string `json:"code"`
		URL              string `json:"url"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode collection link: %v (%s)", err, raw)
	}
	return collectionLinkJSON{collectionRoomID: body.CollectionRoomID, code: body.Code, url: body.URL}
}

func (s *stack) ensureCollectionShareLink(token, roomID string) collectionLinkJSON {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/collection-rooms/"+roomID+"/share-link", nil, token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("ensure collection share link: %d %s", resp.StatusCode, raw)
	}
	return decodeCollectionLink(s.t, raw)
}

func (s *stack) regenerateCollectionShareLink(token, roomID string) collectionLinkJSON {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/collection-rooms/"+roomID+"/share-link/regenerate", nil, token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("regenerate collection share link: %d %s", resp.StatusCode, raw)
	}
	return decodeCollectionLink(s.t, raw)
}

func (s *stack) revokeCollectionShareLink(token, roomID string) int {
	s.t.Helper()
	resp, _ := s.do(http.MethodDelete, "/collection-rooms/"+roomID+"/share-link", nil, token)
	return resp.StatusCode
}

func (s *stack) visitCollectionRoom(code, token string) reply {
	s.t.Helper()
	return s.get("/collection-share-links/"+code+"/collection-room", token)
}

func (s *stack) activeCollectionLinks(roomID string) int {
	s.t.Helper()
	var n int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM collection_share_links WHERE collection_room_id = $1 AND status = 'active'`, roomID).Scan(&n); err != nil {
		s.t.Fatal(err)
	}
	return n
}

type visitorCollectionRoomJSON struct {
	CollectionRoomID string `json:"collection_room_id"`
	Name             string `json:"name"`
	CategoryID       string `json:"category_id"`
	DesignID         string `json:"design_id"`
	CurrentTier      int    `json:"current_tier"`
	Items            []struct {
		ID             string `json:"id"`
		SlotIndex      int    `json:"slot_index"`
		CatalogModelID string `json:"catalog_model_id"`
	} `json:"items"`
}

type collectionShareFixture struct {
	roomA, roomB, itemA string
	link                collectionLinkJSON
	visitor             string
}

func newCollectionShareFixture(t *testing.T, s *stack) collectionShareFixture {
	t.Helper()
	roomA := s.roomWithPublishedDesign(t, "Shared Watches")
	itemA := s.mustAddItem(roomA, syntheticModel).Items[0].ID
	roomB := s.createCollectionRoom(s.token, "Unshared Coins")
	return collectionShareFixture{
		roomA:   roomA,
		roomB:   roomB,
		itemA:   itemA,
		link:    s.ensureCollectionShareLink(s.token, roomA),
		visitor: s.strangerToken(),
	}
}

func TestValidAuthenticatedShareResolvesExactlyOneRoom(t *testing.T) {
	s := newStack(t)
	s.createRoom()
	f := newCollectionShareFixture(t, s)

	if f.link.collectionRoomID != f.roomA || f.link.url != testShareLinkBase+"/c/"+f.link.code || len(f.link.code) != 22 {
		t.Fatalf("link shape: %+v", f.link)
	}

	r := s.visitCollectionRoom(f.link.code, f.visitor)
	if r.status != http.StatusOK {
		t.Fatalf("visitor read: %d %s", r.status, r.body)
	}
	var room visitorCollectionRoomJSON
	dec := json.NewDecoder(strings.NewReader(r.body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&room); err != nil {
		t.Fatalf("the visitor payload carries a field this test does not know about: %v (%s)", err, r.body)
	}
	if room.CollectionRoomID != f.roomA || room.Name != "Shared Watches" || room.CurrentTier < 1 {
		t.Fatalf("wrong Room: %+v", room)
	}
	if len(room.Items) != 1 || room.Items[0].ID != f.itemA || room.Items[0].CatalogModelID != syntheticModel {
		t.Fatalf("items: %+v", room.Items)
	}
	var ownerAccountID string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT account_id::text FROM collection_rooms WHERE id = $1`, f.roomA).Scan(&ownerAccountID); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{ownerAccountID, f.roomB, s.ownMuseumID(s.token), f.link.code, "account", "museum", "privacy", "share", "owner"} {
		if strings.Contains(r.body, forbidden) {
			t.Fatalf("visitor payload must not carry %q: %s", forbidden, r.body)
		}
	}

	if anon := s.visitCollectionRoom(f.link.code, ""); anon.status != http.StatusUnauthorized {
		t.Fatalf("anonymous visitor must be refused with 401, got %d %s", anon.status, anon.body)
	}

	if own := s.visitCollectionRoom(f.link.code, s.token); own.status != http.StatusOK || own.body != r.body {
		t.Fatalf("owner through the link: %d %s", own.status, own.body)
	}

	if again := s.ensureCollectionShareLink(s.token, f.roomA); again != f.link {
		t.Fatalf("ensure is not idempotent: %+v vs %+v", again, f.link)
	}
	if n := s.activeCollectionLinks(f.roomA); n != 1 {
		t.Fatalf("active links for Room A: %d", n)
	}
	if cur := s.get("/collection-rooms/"+f.roomA+"/share-link", s.token); cur.status != http.StatusOK || !strings.Contains(cur.body, f.link.code) {
		t.Fatalf("current link: %d %s", cur.status, cur.body)
	}
}

func TestUUIDWithoutCapabilityGrantsNothing(t *testing.T) {
	s := newStack(t)
	f := newCollectionShareFixture(t, s)
	before := s.roomState(t, f.roomA)

	mustBeIndistinguishable(t, map[string]reply{
		"room A's UUID as the code": s.visitCollectionRoom(f.roomA, f.visitor),
		"room B's UUID as the code": s.visitCollectionRoom(f.roomB, f.visitor),
		"random UUID as the code":   s.visitCollectionRoom(s.randomID(), f.visitor),
		"unknown code":              s.visitCollectionRoom(unknownCode, f.visitor),
		"malformed code":            s.visitCollectionRoom("not-a-code", f.visitor),
		"empty-ish code":            s.visitCollectionRoom("%20", f.visitor),
	})
	if r := s.visitCollectionRoom(unknownCode, f.visitor); r.status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", r.status, r.body)
	}

	if r := s.get("/collection-rooms/"+f.roomA, f.visitor); r.status != http.StatusNotFound {
		t.Fatalf("stranger reading Room A by id: %d %s", r.status, r.body)
	}

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		mustBeIndistinguishable(t, map[string]reply{
			"shared room A":     s.doAs(method, "/collection-rooms/"+f.roomA+"/share-link", f.visitor),
			"unshared room B":   s.doAs(method, "/collection-rooms/"+f.roomB+"/share-link", f.visitor),
			"nonexistent room":  s.doAs(method, "/collection-rooms/"+s.randomID()+"/share-link", f.visitor),
			"malformed room id": s.doAs(method, "/collection-rooms/not-an-id/share-link", f.visitor),
		})
		if r := s.doAs(method, "/collection-rooms/"+f.roomA+"/share-link", f.visitor); r.status != http.StatusNotFound {
			t.Fatalf("%s on a foreign Room's link: %d %s", method, r.status, r.body)
		}
	}
	mustBeIndistinguishable(t, map[string]reply{
		"regenerate shared room A":    s.doAs(http.MethodPost, "/collection-rooms/"+f.roomA+"/share-link/regenerate", f.visitor),
		"regenerate unshared room B":  s.doAs(http.MethodPost, "/collection-rooms/"+f.roomB+"/share-link/regenerate", f.visitor),
		"regenerate nonexistent room": s.doAs(http.MethodPost, "/collection-rooms/"+s.randomID()+"/share-link/regenerate", f.visitor),
	})

	if cur := s.ensureCollectionShareLink(s.token, f.roomA); cur != f.link {
		t.Fatalf("a stranger's attempts changed Room A's link: %+v vs %+v", cur, f.link)
	}
	if n := s.activeCollectionLinks(f.roomB); n != 0 {
		t.Fatalf("a stranger created a link on Room B: %d active", n)
	}
	if after := s.roomState(t, f.roomA); after != before {
		t.Fatalf("Room A changed: %s → %s", before, after)
	}
}

func TestRevokedLinkFailsIndistinguishably(t *testing.T) {
	s := newStack(t)
	f := newCollectionShareFixture(t, s)

	if r := s.visitCollectionRoom(f.link.code, f.visitor); r.status != http.StatusOK {
		t.Fatalf("precondition: live link must resolve, got %d", r.status)
	}
	if status := s.revokeCollectionShareLink(s.token, f.roomA); status != http.StatusNoContent {
		t.Fatalf("revoke: %d", status)
	}

	mustBeIndistinguishable(t, map[string]reply{
		"revoked code":   s.visitCollectionRoom(f.link.code, f.visitor),
		"unknown code":   s.visitCollectionRoom(unknownCode, f.visitor),
		"UUID as code":   s.visitCollectionRoom(f.roomA, f.visitor),
		"garbage code":   s.visitCollectionRoom("revoked-or-not", f.visitor),
		"owner, revoked": s.visitCollectionRoom(f.link.code, s.token),
	})
	if r := s.visitCollectionRoom(f.link.code, f.visitor); r.status != http.StatusNotFound {
		t.Fatalf("revoked link: %d %s", r.status, r.body)
	}
	if cur := s.get("/collection-rooms/"+f.roomA+"/share-link", s.token); cur.status != http.StatusNotFound {
		t.Fatalf("current link after revoke: %d %s", cur.status, cur.body)
	}
	if n := s.activeCollectionLinks(f.roomA); n != 0 {
		t.Fatalf("active links after revoke: %d", n)
	}
	if status := s.revokeCollectionShareLink(s.token, f.roomA); status != http.StatusNoContent {
		t.Fatalf("second revoke: %d", status)
	}
	fresh := s.ensureCollectionShareLink(s.token, f.roomA)
	if fresh.code == f.link.code {
		t.Fatal("re-sharing resurrected the revoked code")
	}
	if r := s.visitCollectionRoom(fresh.code, f.visitor); r.status != http.StatusOK {
		t.Fatalf("fresh link: %d %s", r.status, r.body)
	}
	if r := s.visitCollectionRoom(f.link.code, f.visitor); r.status != http.StatusNotFound {
		t.Fatalf("old link after re-share: %d", r.status)
	}
	if r := s.get("/collection-rooms/"+f.roomA, s.token); r.status != http.StatusOK || !strings.Contains(r.body, f.itemA) {
		t.Fatalf("owner's Room after revoke: %d %s", r.status, r.body)
	}
}

func TestRegenerationInvalidatesPrevious(t *testing.T) {
	s := newStack(t)
	f := newCollectionShareFixture(t, s)
	beforeBody := s.visitCollectionRoom(f.link.code, f.visitor).body

	renewed := s.regenerateCollectionShareLink(s.token, f.roomA)
	if renewed.code == f.link.code || renewed.collectionRoomID != f.roomA {
		t.Fatalf("regenerate returned the same code or another Room: %+v", renewed)
	}

	mustBeIndistinguishable(t, map[string]reply{
		"previous code": s.visitCollectionRoom(f.link.code, f.visitor),
		"unknown code":  s.visitCollectionRoom(unknownCode, f.visitor),
	})
	if r := s.visitCollectionRoom(renewed.code, f.visitor); r.status != http.StatusOK || r.body != beforeBody {
		t.Fatalf("new code must serve the same Room: %d %s", r.status, r.body)
	}
	if cur := s.ensureCollectionShareLink(s.token, f.roomA); cur != renewed {
		t.Fatalf("ensure after regenerate: %+v vs %+v", cur, renewed)
	}
	if n := s.activeCollectionLinks(f.roomA); n != 1 {
		t.Fatalf("active links: %d", n)
	}
	var status string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT status FROM collection_share_links WHERE code = $1`, f.link.code).Scan(&status); err != nil || status != "revoked" {
		t.Fatalf("old row: %q %v", status, err)
	}
}

func TestSharingRoomACannotExposeRoomB(t *testing.T) {
	s := newStack(t)
	s.createRoom()
	f := newCollectionShareFixture(t, s)
	museumID := s.ownMuseumID(s.token)

	visit := s.visitCollectionRoom(f.link.code, f.visitor)
	if visit.status != http.StatusOK || strings.Contains(visit.body, f.roomB) || strings.Contains(visit.body, museumID) {
		t.Fatalf("Room A's payload names Room B or the Museum: %d %s", visit.status, visit.body)
	}

	for name, r := range map[string]reply{
		"B by id (owner route)":        s.get("/collection-rooms/"+f.roomB, f.visitor),
		"B's UUID as a code":           s.visitCollectionRoom(f.roomB, f.visitor),
		"owner's Room list":            s.get("/collection-rooms", f.visitor),
		"owner's Museum by id":         s.get("/museums/"+museumID, f.visitor),
		"owner's Museum share link":    s.get("/museum/me/share-link", f.visitor),
		"A's code on the Museum route": s.get("/share-links/"+f.link.code+"/museum", f.visitor),
	} {
		switch name {
		case "owner's Room list":
			if r.status != http.StatusOK || strings.Contains(r.body, f.roomA) || strings.Contains(r.body, f.roomB) {
				t.Fatalf("%s: %d %s", name, r.status, r.body)
			}
		default:
			if r.status != http.StatusNotFound {
				t.Fatalf("%s: expected 404, got %d %s", name, r.status, r.body)
			}
		}
	}
	if r := s.get("/museum/me/share-link", s.token); r.status != http.StatusNotFound {
		t.Fatalf("a Museum link appeared from sharing a Collection Room: %d %s", r.status, r.body)
	}
	if r := s.get("/collection-rooms/"+f.roomB+"/share-link", s.token); r.status != http.StatusNotFound {
		t.Fatalf("Room B has a link: %d %s", r.status, r.body)
	}
	if n := s.activeCollectionLinks(f.roomB); n != 0 {
		t.Fatalf("Room B active links: %d", n)
	}
}

func TestVisitorCannotMutateOwnerContent(t *testing.T) {
	s := newStack(t)
	f := newCollectionShareFixture(t, s)
	before := s.roomState(t, f.roomA)
	ownerBefore := s.snapshotOwnerState()

	if r := s.visitCollectionRoom(f.link.code, f.visitor); r.status != http.StatusOK {
		t.Fatalf("precondition: the visitor's link must be live, got %d", r.status)
	}

	attempts := []struct {
		name, method, path string
		body               any
	}{
		{"rename", http.MethodPatch, "/collection-rooms/" + f.roomA, map[string]any{"name": "Hijacked"}},
		{"delete room", http.MethodDelete, "/collection-rooms/" + f.roomA, nil},
		{"ratchet tier", http.MethodPost, "/collection-rooms/" + f.roomA + "/tier", map[string]any{"tier": 2}},
		{"add item", http.MethodPost, "/collection-rooms/" + f.roomA + "/items?app_asset_version=1", map[string]any{"catalog_model_id": syntheticModel}},
		{"move item", http.MethodPut, "/collection-rooms/" + f.roomA + "/items/" + f.itemA + "/slot?app_asset_version=1", map[string]any{"slot_index": 1}},
		{"read link", http.MethodGet, "/collection-rooms/" + f.roomA + "/share-link", nil},
		{"ensure link", http.MethodPost, "/collection-rooms/" + f.roomA + "/share-link", nil},
		{"regenerate link", http.MethodPost, "/collection-rooms/" + f.roomA + "/share-link/regenerate", nil},
		{"revoke link", http.MethodDelete, "/collection-rooms/" + f.roomA + "/share-link", nil},
	}
	for _, a := range attempts {
		resp, raw := s.do(a.method, a.path, a.body, f.visitor)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s as visitor: expected 404, got %d %s", a.name, resp.StatusCode, raw)
		}
	}

	if after := s.roomState(t, f.roomA); after != before {
		t.Fatalf("Room A changed under a visitor: %s → %s", before, after)
	}
	if cur := s.ensureCollectionShareLink(s.token, f.roomA); cur != f.link {
		t.Fatalf("Room A's link changed under a visitor: %+v vs %+v", cur, f.link)
	}
	if n := s.activeCollectionLinks(f.roomA); n != 1 {
		t.Fatalf("active links: %d", n)
	}
	if ownerAfter := s.snapshotOwnerState(); ownerAfter != ownerBefore {
		t.Fatalf("owner state changed: %+v → %+v", ownerBefore, ownerAfter)
	}
	if r := s.visitCollectionRoom(f.link.code, f.visitor); r.status != http.StatusOK {
		t.Fatalf("visitor read after attempts: %d", r.status)
	}
}

func TestMuseumAndCollectionSharingAreIndependent(t *testing.T) {
	s := newStack(t)
	_, museumLink := newLinkedFixture(t, s)
	f := newCollectionShareFixture(t, s)

	mustBeIndistinguishable(t, map[string]reply{
		"museum code on the collection route":  s.visitCollectionRoom(museumLink.code, f.visitor),
		"unknown code on the collection route": s.visitCollectionRoom(unknownCode, f.visitor),
	})
	mustBeIndistinguishable(t, map[string]reply{
		"collection code on the museum preview": s.get("/share-links/"+f.link.code, ""),
		"unknown code on the museum preview":    s.get("/share-links/"+unknownCode, ""),
	})
	mustBeIndistinguishable(t, map[string]reply{
		"collection code on the museum content route": s.get("/share-links/"+f.link.code+"/museum", f.visitor),
		"unknown code on the museum content route":    s.get("/share-links/"+unknownCode+"/museum", f.visitor),
	})
	if r := s.visitCollectionRoom(museumLink.code, f.visitor); r.status != http.StatusNotFound {
		t.Fatalf("museum code on the collection route: %d", r.status)
	}

	museumOK := func() bool {
		return s.get("/share-links/"+museumLink.code+"/museum", f.visitor).status == http.StatusOK
	}
	collectionOK := func() bool { return s.visitCollectionRoom(f.link.code, f.visitor).status == http.StatusOK }
	if !museumOK() || !collectionOK() {
		t.Fatal("precondition: both links live")
	}
	renewedMuseum := s.regenerateShareLink(s.token)
	if !collectionOK() {
		t.Fatal("regenerating the Museum link broke the Collection link")
	}
	museumLink = renewedMuseum
	renewedCollection := s.regenerateCollectionShareLink(s.token, f.roomA)
	if !museumOK() {
		t.Fatal("regenerating the Collection link broke the Museum link")
	}
	f.link = renewedCollection
	if s.revokeCollectionShareLink(s.token, f.roomA) != http.StatusNoContent || !museumOK() {
		t.Fatal("revoking the Collection link broke the Museum link")
	}
	s.setMuseumPrivacy(s.token, "private")
	restored := s.ensureCollectionShareLink(s.token, f.roomA)
	if s.visitCollectionRoom(restored.code, f.visitor).status != http.StatusOK {
		t.Fatal("making the Museum Private closed a Collection link — Collection Rooms have no privacy state and does not apply to them")
	}
	if museumOK() {
		t.Fatal("precondition: a Private Museum's link must be closed")
	}

	var n int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM share_links WHERE code = ANY($1)) + (SELECT count(*) FROM collection_share_links WHERE code = ANY($2))`,
		[]string{f.link.code, restored.code}, []string{museumLink.code}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d code(s) found in the other surface's table", n)
	}
	for table, want := range map[string]string{"share_links": "museums", "collection_share_links": "collection_rooms"} {
		rows, err := s.pool.Pool().Query(context.Background(),
			`SELECT confrelid::regclass::text FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'f' ORDER BY 1`, table)
		if err != nil {
			t.Fatal(err)
		}
		var targets []string
		for rows.Next() {
			var target string
			if err := rows.Scan(&target); err != nil {
				t.Fatal(err)
			}
			targets = append(targets, target)
		}
		rows.Close()
		if len(targets) != 1 || targets[0] != want {
			t.Fatalf("%s references %v, want exactly [%s]", table, targets, want)
		}
	}
}

func TestOneActiveLinkPerRoomUnderConcurrency(t *testing.T) {
	s := newStack(t)
	roomID := s.createCollectionRoom(s.token, "Raced Coins")

	const racers = 8
	ensure := make([]collectionLinkJSON, racers)
	var wg sync.WaitGroup
	for i := range ensure {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, raw := s.do(http.MethodPost, "/collection-rooms/"+roomID+"/share-link", nil, s.token)
			if resp.StatusCode == http.StatusOK {
				ensure[i] = decodeCollectionLink(t, raw)
			}
		}(i)
	}
	wg.Wait()
	for i, l := range ensure {
		if l.code == "" || l.code != ensure[0].code {
			t.Fatalf("concurrent ensure %d: %+v vs %+v", i, l, ensure[0])
		}
	}
	if n := s.activeCollectionLinks(roomID); n != 1 {
		t.Fatalf("active after concurrent ensure: %d", n)
	}

	rotated := make([]collectionLinkJSON, racers)
	for i := range rotated {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, raw := s.do(http.MethodPost, "/collection-rooms/"+roomID+"/share-link/regenerate", nil, s.token)
			if resp.StatusCode == http.StatusOK {
				rotated[i] = decodeCollectionLink(t, raw)
			}
		}(i)
	}
	wg.Wait()
	if n := s.activeCollectionLinks(roomID); n != 1 {
		t.Fatalf("active after concurrent regenerate: %d", n)
	}
	visitor := s.strangerToken()
	live := 0
	for _, l := range rotated {
		if l.code == "" {
			t.Fatal("a concurrent regenerate failed")
		}
		if s.visitCollectionRoom(l.code, visitor).status == http.StatusOK {
			live++
		}
	}
	if live != 1 || s.visitCollectionRoom(ensure[0].code, visitor).status != http.StatusNotFound {
		t.Fatalf("exactly one of the rotated codes must be live and the original dead; live=%d", live)
	}
}

func TestAppSiteAssociationCoversCollectionLinks(t *testing.T) {
	s := newStack(t)
	r := s.get("/.well-known/apple-app-site-association", "")
	if r.status != http.StatusOK {
		t.Fatalf("AASA: %d %s", r.status, r.body)
	}
	for _, path := range []string{`"/m/*"`, `"/c/*"`} {
		if !strings.Contains(r.body, path) {
			t.Fatalf("AASA must associate %s: %s", path, r.body)
		}
	}
}

func (s *stack) doAs(method, path, token string) reply {
	s.t.Helper()
	resp, raw := s.do(method, path, nil, token)
	return reply{status: resp.StatusCode, body: string(raw)}
}
