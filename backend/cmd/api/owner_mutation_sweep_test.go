package main

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

type mutationClass int

const (
	targetsOwnersResource mutationClass = iota
	callerScoped
)

var mutationClassification = map[string]mutationClass{
	"POST /analytics/events":                                                 callerScoped,
	"POST /museum":                                                           callerScoped,
	"PATCH /museum/me/style":                                                 callerScoped,
	"PATCH /museum/me/privacy":                                               callerScoped,
	"POST /museum/me/rooms":                                                  callerScoped,
	"PATCH /museum/me/rooms/{roomID}":                                        targetsOwnersResource,
	"DELETE /museum/me/rooms/{roomID}":                                       targetsOwnersResource,
	"POST /museum/me/rooms/{roomID}/photos":                                  targetsOwnersResource,
	"PUT /museum/me/rooms/{roomID}/photo-order":                              targetsOwnersResource,
	"PUT /museum/me/rooms/{roomID}/photos/{photoAssetID}/caption":            targetsOwnersResource,
	"POST /museum/me/rooms/{roomID}/photos/{photoAssetID}/replacement":       targetsOwnersResource,
	"DELETE /museum/me/rooms/{roomID}/photos/{photoAssetID}":                 targetsOwnersResource,
	"POST /museum/me/rooms/{roomID}/sculptures":                              targetsOwnersResource,
	"DELETE /museum/me/rooms/{roomID}/sculptures/{slotIndex}":                targetsOwnersResource,
	"PUT /museum/me/rooms/{roomID}/music":                                    targetsOwnersResource,
	"DELETE /museum/me/rooms/{roomID}/music":                                 targetsOwnersResource,
	"POST /museum/me/share-link":                                             callerScoped,
	"POST /museum/me/share-link/regenerate":                                  callerScoped,
	"POST /media/photo-uploads":                                              callerScoped,
	"PATCH /profile/me":                                                      callerScoped,
	"POST /collection-rooms":                                                 callerScoped,
	"PATCH /collection-rooms/{collectionRoomID}":                             targetsOwnersResource,
	"DELETE /collection-rooms/{collectionRoomID}":                            targetsOwnersResource,
	"POST /collection-rooms/{collectionRoomID}/tier":                         targetsOwnersResource,
	"POST /collection-rooms/{collectionRoomID}/items":                        targetsOwnersResource,
	"PUT /collection-rooms/{collectionRoomID}/items/{collectionItemID}/slot": targetsOwnersResource,
	"PUT /collection-rooms/{collectionRoomID}/music":                         targetsOwnersResource,
	"DELETE /collection-rooms/{collectionRoomID}/music":                      targetsOwnersResource,
	"POST /collection-rooms/{collectionRoomID}/share-link":                   targetsOwnersResource,
	"POST /collection-rooms/{collectionRoomID}/share-link/regenerate":        targetsOwnersResource,
	"DELETE /collection-rooms/{collectionRoomID}/share-link":                 targetsOwnersResource,
	"POST /entitlements/app-account-token":                                   callerScoped,
	"POST /entitlements/app-store/transactions":                              callerScoped,
}

var ownerScopedReads = []string{
	"GET /museum/me/rooms/{roomID}",
	"GET /museum/me/rooms/{roomID}/photo-urls",
	"GET /collection-rooms/{collectionRoomID}",
	"GET /collection-rooms/{collectionRoomID}/share-link",
}

func TestEveryProtectedMutatingRouteIsClassified(t *testing.T) {
	var mutating []string
	for pattern, class := range routeClassification {
		if class != protectedContent || strings.HasPrefix(pattern, "GET ") {
			continue
		}
		mutating = append(mutating, pattern)
	}
	sort.Strings(mutating)
	if len(mutating) < 30 {
		t.Fatalf("expected the full mutating route table, found only %d", len(mutating))
	}
	for _, pattern := range mutating {
		if _, ok := mutationClassification[pattern]; !ok {
			t.Errorf("mutating route %q is protected but not classified for the owner-only sweep — add it to mutationClassification and say whose resource it acts on", pattern)
		}
	}
	for pattern := range mutationClassification {
		if class, ok := routeClassification[pattern]; !ok || class != protectedContent {
			t.Errorf("%q is classified for the mutation sweep but is not a protected route in the inventory", pattern)
		}
	}
	for _, pattern := range ownerScopedReads {
		if class, ok := routeClassification[pattern]; !ok || class != protectedContent {
			t.Errorf("owner-scoped read %q is not a protected route in the inventory", pattern)
		}
	}
}

type nonOwners struct {
	bare          string
	resourceOwner string
	visitor       string
}

func newNonOwners(t *testing.T, s *stack, f sweepFixture) nonOwners {
	t.Helper()
	n := nonOwners{bare: s.strangerToken(), resourceOwner: s.strangerToken(), visitor: s.strangerToken()}

	if resp, raw := s.do(http.MethodPost, "/museum", map[string]string{"style_id": "style_modern"}, n.resourceOwner); resp.StatusCode != http.StatusCreated {
		t.Fatalf("resource owner's museum: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := s.do(http.MethodPost, "/museum/me/rooms", map[string]string{"name": "Theirs", "variant_id": "style_modern_variant_Hall"}, n.resourceOwner); resp.StatusCode != http.StatusCreated {
		t.Fatalf("resource owner's room: %d %s", resp.StatusCode, raw)
	}
	s.createCollectionRoomAs(t, n.resourceOwner)

	if r := s.get("/share-links/"+f.code+"/museum", n.visitor); r.status != http.StatusOK {
		t.Fatalf("visitor's Museum link must resolve: %d %s", r.status, r.body)
	}
	if r := s.visitCollectionRoom(f.collectionCode, n.visitor); r.status != http.StatusOK {
		t.Fatalf("visitor's Collection link must resolve: %d %s", r.status, r.body)
	}
	return n
}

func instantiateMutation(f sweepFixture, pattern string, real bool) (method, path string, body any) {
	method, path, body = f.instantiate(pattern, real)
	switch {
	case strings.HasSuffix(pattern, "/music") && method == http.MethodPut:
		body = map[string]string{"music_track_id": "track_sweep_dev"}
	case strings.HasSuffix(pattern, "/replacement"):
		body = map[string]string{"asset_id": "11111111-1111-4111-8111-111111111111"}
	}
	return method, path, body
}

func (s *stack) linkState(f sweepFixture) string {
	return s.get("/museum/me/share-link", s.token).body + "|" +
		s.ensureCollectionShareLink(s.token, f.collectionRoomID).code
}

func TestNonOwnerMutations_AreIndistinguishableFromNonexistent_AndChangeNothing(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	n := newNonOwners(t, s, f)
	before := s.snapshotOwnerState()
	linksBefore := s.linkState(f)

	callers := map[string]string{"bare stranger": n.bare, "resource-owning stranger": n.resourceOwner, "visitor with live links": n.visitor}
	exercised := 0
	for pattern, class := range mutationClassification {
		if class != targetsOwnersResource {
			continue
		}
		for callerName, token := range callers {
			method, realPath, realBody := instantiateMutation(f, pattern, true)
			_, bogusPath, bogusBody := instantiateMutation(f, pattern, false)

			realResp, realRaw := s.do(method, realPath, realBody, token)
			bogusResp, bogusRaw := s.do(method, bogusPath, bogusBody, token)
			label := pattern + " as " + callerName

			if realResp.StatusCode != http.StatusNotFound {
				t.Errorf("%s → %d %s; a non-owner must get 404", label, realResp.StatusCode, realRaw)
			}
			if realResp.StatusCode != bogusResp.StatusCode || string(realRaw) != string(bogusRaw) {
				t.Errorf("%s: the owner's real resource (%d %s) is distinguishable from a nonexistent one (%d %s)",
					label, realResp.StatusCode, realRaw, bogusResp.StatusCode, bogusRaw)
			}
			exercised++
		}
	}
	if exercised == 0 {
		t.Fatal("no resource-targeting mutation was exercised")
	}

	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("non-owner mutations changed the owner's rows:\nbefore %+v\nafter  %+v", before, after)
	}
	if linksAfter := s.linkState(f); linksAfter != linksBefore {
		t.Fatalf("non-owner mutations changed the owner's share links: %s → %s", linksBefore, linksAfter)
	}
	if n := s.activeCollectionLinks(f.collectionRoomID); n != 1 {
		t.Fatalf("owner's Collection Room must still have exactly one active link, has %d", n)
	}
	if r := s.visitCollectionRoom(f.collectionCode, n.visitor); r.status != http.StatusOK {
		t.Fatalf("visitor read after refused mutations: %d", r.status)
	}
}

func TestCallerScopedMutations_CannotReachAnotherAccount(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	n := newNonOwners(t, s, f)
	before := s.snapshotOwnerState()
	linksBefore := s.linkState(f)

	patterns := make([]string, 0)
	for pattern, class := range mutationClassification {
		if class == callerScoped {
			patterns = append(patterns, pattern)
		}
	}
	sort.Strings(patterns)
	for _, pattern := range patterns {
		method, path, body := instantiateMutation(f, pattern, true)
		resp, raw := s.do(method, path, body, n.resourceOwner)
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("%s as a stranger on their own account → %d %s", pattern, resp.StatusCode, raw)
		}
		if after := s.snapshotOwnerState(); after != before {
			t.Fatalf("%s by a stranger changed the OWNER's rows:\nbefore %+v\nafter  %+v", pattern, before, after)
		}
	}
	if linksAfter := s.linkState(f); linksAfter != linksBefore {
		t.Fatalf("a stranger's own share-link operations changed the owner's links: %s → %s", linksBefore, linksAfter)
	}
}

func TestOwnerScopedReads_HideForeignResourcesAcrossEveryContentType(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	n := newNonOwners(t, s, f)

	callers := map[string]string{"bare stranger": n.bare, "resource-owning stranger": n.resourceOwner, "visitor with live links": n.visitor}
	for _, pattern := range ownerScopedReads {
		for callerName, token := range callers {
			_, realPath, _ := f.instantiate(pattern, true)
			_, bogusPath, _ := f.instantiate(pattern, false)
			real := s.get(realPath, token)
			bogus := s.get(bogusPath, token)
			label := pattern + " as " + callerName
			if real.status != http.StatusNotFound {
				t.Errorf("%s → %d %s; a non-owner must get 404", label, real.status, real.body)
			}
			if real != bogus {
				t.Errorf("%s: real (%d %s) vs nonexistent (%d %s) differ", label, real.status, real.body, bogus.status, bogus.body)
			}
		}
	}
	for _, pattern := range ownerScopedReads {
		_, realPath, _ := f.instantiate(pattern, true)
		if r := s.get(realPath, s.token); r.status != http.StatusOK {
			t.Errorf("owner reading %s → %d %s", pattern, r.status, r.body)
		}
	}
}
