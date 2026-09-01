package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type routeClass int

const (
	protectedContent routeClass = iota
	publicByDesign
	authenticationEndpoint
	devSignedStorage
	devPublicPlatformAssets
	monitoringToken
)

var routeClassification = map[string]routeClass{
	"POST /museum":                                                     protectedContent,
	"GET /museum/me":                                                   protectedContent,
	"PATCH /museum/me/style":                                           protectedContent,
	"PATCH /museum/me/privacy":                                         protectedContent,
	"POST /museum/me/rooms":                                            protectedContent,
	"GET /museum/me/rooms":                                             protectedContent,
	"GET /museum/me/rooms/{roomID}":                                    protectedContent,
	"PATCH /museum/me/rooms/{roomID}":                                  protectedContent,
	"DELETE /museum/me/rooms/{roomID}":                                 protectedContent,
	"POST /museum/me/rooms/{roomID}/photos":                            protectedContent,
	"GET /museum/me/rooms/{roomID}/photo-urls":                         protectedContent,
	"PUT /museum/me/rooms/{roomID}/photo-order":                        protectedContent,
	"PUT /museum/me/rooms/{roomID}/photos/{photoAssetID}/caption":      protectedContent,
	"POST /museum/me/rooms/{roomID}/photos/{photoAssetID}/replacement": protectedContent,
	"DELETE /museum/me/rooms/{roomID}/photos/{photoAssetID}":           protectedContent,
	"POST /museum/me/rooms/{roomID}/sculptures":                        protectedContent,
	"DELETE /museum/me/rooms/{roomID}/sculptures/{slotIndex}":          protectedContent,
	"POST /museum/me/share-link":                                       protectedContent,
	"GET /museum/me/share-link":                                        protectedContent,
	"POST /museum/me/share-link/regenerate":                            protectedContent,
	"POST /media/photo-uploads":                                        protectedContent,
	"GET /profile/me":                                                  protectedContent,
	"PATCH /profile/me":                                                protectedContent,
	"GET /profile/{accountID}":                                         protectedContent,
	"GET /museums/{museumID}":                                          protectedContent,
	"GET /museums/{museumID}/rooms/{roomID}":                           protectedContent,
	"GET /share-links/{code}/museum":                                   protectedContent,
	"GET /share-links/{code}/rooms/{roomID}":                           protectedContent,
	"GET /share-links/{code}/rooms/{roomID}/photo-urls":                protectedContent,
	"GET /catalog/styles":                                              protectedContent,
	"GET /catalog/styles/{styleID}/variants":                           protectedContent,
	"GET /catalog/sculptures":                                          protectedContent,
	"GET /catalog/music":                                               protectedContent,
	"GET /catalog/music/{trackID}/audio-url":                           protectedContent,
	"PUT /museum/me/rooms/{roomID}/music":                              protectedContent,
	"DELETE /museum/me/rooms/{roomID}/music":                           protectedContent,
	"GET /catalog/bundles/{bundleID}/manifest":                         protectedContent,
	"GET /catalog/room-variants/{variantID}":                           protectedContent,
	"GET /catalog/collection-categories":                               protectedContent,
	"GET /catalog/collection-presentation-assets":                      protectedContent,
	"GET /catalog/collection-designs":                                  protectedContent,
	"GET /catalog/collection-models":                                   protectedContent,

	"POST /collection-rooms":                                                 protectedContent,
	"GET /collection-rooms":                                                  protectedContent,
	"GET /collection-rooms/{collectionRoomID}":                               protectedContent,
	"PATCH /collection-rooms/{collectionRoomID}":                             protectedContent,
	"DELETE /collection-rooms/{collectionRoomID}":                            protectedContent,
	"POST /collection-rooms/{collectionRoomID}/tier":                         protectedContent,
	"POST /collection-rooms/{collectionRoomID}/items":                        protectedContent,
	"PUT /collection-rooms/{collectionRoomID}/items/{collectionItemID}/slot": protectedContent,
	"GET /entitlements/me":                                                   protectedContent,
	"POST /entitlements/app-account-token":                                   protectedContent,
	"POST /entitlements/app-store/transactions":                              protectedContent,
	"POST /analytics/events":                                                 protectedContent,
	"PUT /collection-rooms/{collectionRoomID}/music":                         protectedContent,
	"DELETE /collection-rooms/{collectionRoomID}/music":                      protectedContent,
	"POST /collection-rooms/{collectionRoomID}/share-link":                   protectedContent,
	"GET /collection-rooms/{collectionRoomID}/share-link":                    protectedContent,
	"POST /collection-rooms/{collectionRoomID}/share-link/regenerate":        protectedContent,
	"DELETE /collection-rooms/{collectionRoomID}/share-link":                 protectedContent,
	"GET /collection-share-links/{code}/collection-room":                     protectedContent,
	"GET /health":       publicByDesign,
	"GET /health/ready": publicByDesign,
	"GET /metrics":      monitoringToken,
	"GET /.well-known/apple-app-site-association": publicByDesign,
	"GET /share-links/{code}":                     publicByDesign,
	"GET /m/{code}":                               publicByDesign,
	"GET /c/{code}":                               publicByDesign,
	"POST /app-store/notifications":               publicByDesign,
	"POST /auth/apple":                            authenticationEndpoint,
	"POST /auth/google":                           authenticationEndpoint,
	"POST /auth/refresh":                          authenticationEndpoint,
	"POST /auth/logout":                           authenticationEndpoint,
	"POST /auth/email/signup":                     authenticationEndpoint,
	"POST /auth/email/verify":                     authenticationEndpoint,
	"POST /auth/email/verification/resend":        authenticationEndpoint,
	"POST /auth/email/login":                      authenticationEndpoint,
	"POST /auth/email/password-reset":             authenticationEndpoint,
	"POST /auth/email/password-reset/confirm":     authenticationEndpoint,
	"GET /dev-storage/{key...}":                   devSignedStorage,
	"PUT /dev-storage/{key...}":                   devSignedStorage,
	"GET /dev-assets/{key...}":                    devPublicPlatformAssets,
}

func routeInventory(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../internal/*/interfaces/*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "../../cmd/api/main.go")

	literal := regexp.MustCompile(`router\.Handle\("([A-Z]+ /[^"]*)"`)
	concatenated := regexp.MustCompile(`router\.Handle\("([A-Z]+) "\+mediainfra\.(\w+)\+"\{key\.\.\.\}"`)
	prefixConstants := map[string]string{
		"DevStoragePathPrefix":     "/dev-storage/",
		"DevPublicAssetPathPrefix": "/dev-assets/",
	}

	seen := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range literal.FindAllStringSubmatch(string(src), -1) {
			seen[m[1]] = true
		}
		for _, m := range concatenated.FindAllStringSubmatch(string(src), -1) {
			prefix, known := prefixConstants[m[2]]
			if !known {
				t.Fatalf("route registered with unknown path-prefix constant mediainfra.%s in %s — "+
					"add it to prefixConstants so the sweep can see the route", m[2], file)
			}
			seen[m[1]+" "+prefix+"{key...}"] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("route inventory found nothing — the parser is broken, not the router")
	}
	patterns := make([]string, 0, len(seen))
	for p := range seen {
		patterns = append(patterns, p)
	}
	sort.Strings(patterns)
	return patterns
}

func TestEveryRegisteredRouteIsClassified(t *testing.T) {
	inventory := routeInventory(t)

	for _, pattern := range inventory {
		if _, ok := routeClassification[pattern]; !ok {
			t.Errorf("route %q is registered but not classified — add it to routeClassification and say what it is", pattern)
		}
	}
	for pattern := range routeClassification {
		found := false
		for _, p := range inventory {
			if p == pattern {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("route %q is classified but no longer registered — remove the stale entry", pattern)
		}
	}
	if len(inventory) < 40 {
		t.Fatalf("expected the full route table, found only %d patterns", len(inventory))
	}
}

type sweepFixture struct {
	gate2Fixture
	ownerAccountID   string
	code             string
	assetID          string
	collectionRoomID string
	collectionItemID string
	collectionCode   string
}

func newSweepFixture(t *testing.T, s *stack) sweepFixture {
	t.Helper()
	f := newGate2Fixture(t, s)
	s.setMuseumPrivacy(s.token, "public")
	link := s.ensureShareLink(s.token)
	photo := s.uploaded(newPhoto(t, 640, 480, "sweep-photo-1"))
	if resp, _, body := s.assign(f.publicRoom, []string{photo.asset}); resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign photo: %d %v", resp.StatusCode, body)
	}
	collectionRoomID := s.roomWithPublishedDesign(t, "Sweep Watches")
	return sweepFixture{
		gate2Fixture:     f,
		ownerAccountID:   s.accountID,
		code:             link.code,
		assetID:          photo.asset,
		collectionRoomID: collectionRoomID,
		collectionItemID: s.mustAddItem(collectionRoomID, syntheticModel).Items[0].ID,
		collectionCode:   s.ensureCollectionShareLink(s.token, collectionRoomID).code,
	}
}

const bogusUUID = "00000000-0000-4000-8000-000000000000"

func (f sweepFixture) instantiate(pattern string, real bool) (method, path string, body any) {
	parts := strings.SplitN(pattern, " ", 2)
	method, path = parts[0], parts[1]

	pick := func(realValue, bogusValue string) string {
		if real {
			return realValue
		}
		return bogusValue
	}
	replacements := map[string]string{
		"{roomID}":       pick(f.publicRoom, bogusUUID),
		"{photoAssetID}": pick(f.assetID, bogusUUID),
		"{slotIndex}":    "0",
		"{styleID}":      pick("style_modern", "style_nope"),
		"{museumID}":     pick(f.museumID, bogusUUID),
		"{accountID}":    pick(f.ownerAccountID, bogusUUID),
		"{code}":         pick(f.code, unknownCode),
		"{trackID}":      pick("track_sweep_dev", "track_nope"),

		"{collectionRoomID}": pick(f.collectionRoomID, bogusUUID),
		"{collectionItemID}": pick(f.collectionItemID, bogusUUID),
	}
	for placeholder, value := range replacements {
		path = strings.ReplaceAll(path, placeholder, value)
	}

	switch {
	case pattern == "POST /museum":
		body = map[string]string{"style_id": "style_modern"}
	case pattern == "POST /museum/me/rooms":
		body = map[string]string{"name": "Sweep", "variant_id": "style_modern_variant_Hall"}
	case pattern == "PATCH /museum/me/style":
		body = map[string]string{"style_id": "style_modern"}
	case pattern == "PATCH /museum/me/privacy", pattern == "PATCH /museum/me/rooms/{roomID}":
		body = map[string]string{"privacy": "public"}
	case pattern == "PATCH /profile/me":
		body = map[string]string{"avatar_id": "avatar_1"}
	case pattern == "POST /media/photo-uploads":
		body = map[string]any{"client_upload_id": "sweep", "content_type": "image/jpeg", "byte_size": 100, "pixel_width": 10, "pixel_height": 10, "checksum_sha256": strings.Repeat("0", 64)}
	case pattern == "POST /museum/me/rooms/{roomID}/photos":
		body = map[string]any{"asset_ids": []string{pick(f.assetID, bogusUUID)}}
	case strings.HasSuffix(pattern, "/replacement"):
		body = map[string]string{"asset_id": pick(f.assetID, bogusUUID)}
	case pattern == "POST /museum/me/rooms/{roomID}/sculptures":
		body = map[string]string{"catalog_id": "sculpture_x"}
	case pattern == "PUT /museum/me/rooms/{roomID}/music":
		body = map[string]string{"music_track_id": pick("track_sweep_dev", "track_nope")}
	case strings.HasSuffix(pattern, "/photo-order"):
		body = map[string]any{"photo_asset_ids": []string{pick(f.assetID, bogusUUID)}}
	case strings.HasSuffix(pattern, "/caption"):
		body = map[string]string{"caption": "sweep"}
	case pattern == "POST /collection-rooms":
		body = map[string]string{"name": "Sweep Watches", "category_id": "watches"}
	case pattern == "PATCH /collection-rooms/{collectionRoomID}":
		body = map[string]string{"name": "Renamed by nobody"}
	case pattern == "POST /collection-rooms/{collectionRoomID}/tier":
		body = map[string]any{"tier": 2}
	case pattern == "POST /collection-rooms/{collectionRoomID}/items":
		body = map[string]string{"catalog_model_id": pick(syntheticModel, "model_nope")}
	case strings.HasSuffix(pattern, "/items/{collectionItemID}/slot"):
		body = map[string]any{"slot_index": 1}
	}
	return method, path, body
}

func (s *stack) doAnonymous(method, path string, body any, headers map[string]string) reply {
	s.t.Helper()
	var payload io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.server.URL+path, payload)
	if err != nil {
		s.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, body: string(raw)}
}

func anonymousAttempts(code string, path string) map[string]struct {
	path    string
	headers map[string]string
} {
	type attempt = struct {
		path    string
		headers map[string]string
	}
	return map[string]attempt{
		"no token":              {path, nil},
		"garbage bearer":        {path, map[string]string{"Authorization": "Bearer not-a-token"}},
		"empty bearer":          {path, map[string]string{"Authorization": "Bearer "}},
		"share code as bearer":  {path, map[string]string{"Authorization": "Bearer " + code}},
		"share code in query":   {path + "?code=" + code + "&share_link=" + code, nil},
		"share code in headers": {path, map[string]string{"X-Muse-Share-Link": code, "X-Share-Code": code, "Cookie": "share_link=" + code}},
		"basic auth":            {path, map[string]string{"Authorization": "Basic b3duZXI6b3duZXI="}},
	}
}

func TestUnauthenticatedSweep_EveryProtectedRouteRefusesIdentically(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)
	before := s.snapshotOwnerState()
	linkBefore := s.get("/museum/me/share-link", s.token).body

	var reference *reply
	for pattern, class := range routeClassification {
		if class != protectedContent {
			continue
		}
		for _, real := range []bool{true, false} {
			method, path, body := f.instantiate(pattern, real)
			for name, attempt := range anonymousAttempts(f.code, path) {
				label := fmt.Sprintf("%s [real=%v] %s", pattern, real, name)
				r := s.doAnonymous(method, attempt.path, body, attempt.headers)
				if r.status != http.StatusUnauthorized {
					t.Errorf("%s → %d %s; want 401", label, r.status, r.body)
					continue
				}
				if reference == nil {
					reference = &r
					continue
				}
				if r.body != reference.body {
					t.Errorf("%s → body %q differs from the canonical refusal %q", label, r.body, reference.body)
				}
			}
		}
	}
	if reference == nil {
		t.Fatal("no protected route was exercised")
	}
	if !strings.Contains(reference.body, "authentication required") {
		t.Fatalf("the canonical refusal must be the plain authentication message, got %q", reference.body)
	}

	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("anonymous requests must change nothing of the owner's:\nbefore %+v\nafter  %+v", before, after)
	}
	if linkAfter := s.get("/museum/me/share-link", s.token).body; linkAfter != linkBefore {
		t.Fatalf("anonymous requests must not touch the share link: %s vs %s", linkBefore, linkAfter)
	}
}

func TestShareCodeAloneIsNeverAuthorityForContent(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)

	if r := s.get("/share-links/"+f.code+"/museum", f.stranger); r.status != http.StatusOK {
		t.Fatalf("live link with a token must resolve: %d %s", r.status, r.body)
	}

	replies := map[string]reply{}
	for _, path := range []string{
		"/share-links/" + f.code + "/museum",
		"/share-links/" + f.code + "/rooms/" + f.publicRoom,
		"/share-links/" + unknownCode + "/museum",
		"/museums/" + f.museumID,
		"/museum/me/rooms/" + f.publicRoom + "/photo-urls",
	} {
		for name, attempt := range anonymousAttempts(f.code, path) {
			replies[path+" "+name] = s.doAnonymous(http.MethodGet, attempt.path, nil, attempt.headers)
		}
	}
	mustBeIndistinguishable(t, replies)
	for label, r := range replies {
		if r.status != http.StatusUnauthorized {
			t.Fatalf("%s → %d; a share code must never stand in for a session", label, r.status)
		}
	}
}

func TestPublicSurfaceIsExactlyTheDeclaredOne(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)

	var public []string
	for pattern, class := range routeClassification {
		if class == publicByDesign {
			public = append(public, pattern)
		}
	}
	sort.Strings(public)
	want := []string{
		"GET /.well-known/apple-app-site-association",
		"GET /c/{code}",
		"GET /health",
		"GET /health/ready",
		"GET /m/{code}",
		"GET /share-links/{code}",
		"POST /app-store/notifications",
	}
	if strings.Join(public, ",") != strings.Join(want, ",") {
		t.Fatalf("the public surface changed: %v — this is a security decision, not a convenience", public)
	}

	if r := s.doAnonymous(http.MethodGet, "/health", nil, nil); r.status != http.StatusOK {
		t.Fatalf("health: %d", r.status)
	}
	if r := s.doAnonymous(http.MethodGet, "/.well-known/apple-app-site-association", nil, nil); r.status != http.StatusOK {
		t.Fatalf("AASA: %d", r.status)
	}

	preview := s.doAnonymous(http.MethodGet, "/share-links/"+f.code, nil, nil)
	if preview.status != http.StatusOK {
		t.Fatalf("preview of a live link: %d %s", preview.status, preview.body)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(preview.body), &body); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "code,owner,style_id" {
		t.Fatalf("preview keys changed: %v", keys)
	}
	owner, _ := body["owner"].(map[string]any)
	if len(owner) != 1 || owner["avatar_id"] == nil {
		t.Fatalf("preview owner must carry avatar_id only: %s", preview.body)
	}
	for _, forbidden := range []string{"display_name", f.museumID, f.publicRoom, f.privateRoom, "rooms", "privacy", "photo"} {
		if strings.Contains(preview.body, forbidden) {
			t.Fatalf("preview must not carry %q: %s", forbidden, preview.body)
		}
	}
	mustBeIndistinguishable(t, map[string]reply{
		"unknown preview": s.doAnonymous(http.MethodGet, "/share-links/"+unknownCode, nil, nil),
		"malformed":       s.doAnonymous(http.MethodGet, "/share-links/abc", nil, nil),
	})
	landing := s.doAnonymous(http.MethodGet, "/m/"+f.code, nil, nil)
	if landing.status != http.StatusOK || strings.Contains(landing.body, "owner") || strings.Contains(landing.body, f.publicRoom) {
		t.Fatalf("landing must be the nameless page: %d %s", landing.status, landing.body)
	}
	normalise := func(body, code string) string { return strings.ReplaceAll(body, code, "{code}") }
	liveCollection := s.doAnonymous(http.MethodGet, "/c/"+f.collectionCode, nil, nil)
	deadCollection := s.doAnonymous(http.MethodGet, "/c/"+unknownCode, nil, nil)
	if liveCollection.status != http.StatusOK || deadCollection.status != http.StatusOK {
		t.Fatalf("collection landing: live %d, dead %d — both must be the same constant page", liveCollection.status, deadCollection.status)
	}
	if normalise(liveCollection.body, f.collectionCode) != normalise(deadCollection.body, unknownCode) {
		t.Fatal("the Collection landing page differs between a live and a dead code — it must confirm nothing")
	}
	for _, forbidden := range []string{f.collectionRoomID, f.museumID, "owner", "Watches"} {
		if strings.Contains(liveCollection.body, forbidden) {
			t.Fatalf("the Collection landing must not carry %q", forbidden)
		}
	}
	for name, body := range map[string]any{
		"empty":        map[string]string{},
		"not a JWS":    map[string]string{"signedPayload": "premium"},
		"three parts":  map[string]string{"signedPayload": "eyJhbGciOiJFUzI1NiJ9.e30.AAAA"},
		"client claim": map[string]string{"isPremium": "true"},
	} {
		if r := s.doAnonymous(http.MethodPost, "/app-store/notifications", body, nil); r.status != http.StatusBadRequest {
			t.Fatalf("notification %s: %d %s — an unverifiable payload must be refused", name, r.status, r.body)
		}
	}
}

func TestDevStorageBytesRequireASignedURL(t *testing.T) {
	s := newStack(t)
	f := newSweepFixture(t, s)

	tickets := s.get("/museum/me/rooms/"+f.publicRoom+"/photo-urls", s.token)
	if tickets.status != http.StatusOK {
		t.Fatalf("tickets: %d %s", tickets.status, tickets.body)
	}
	var decoded struct {
		Tickets []struct {
			URL string `json:"url"`
		} `json:"tickets"`
	}
	_ = json.Unmarshal([]byte(tickets.body), &decoded)
	if len(decoded.Tickets) != 1 {
		t.Fatalf("expected one ticket, got %s", tickets.body)
	}
	signed := decoded.Tickets[0].URL
	unsigned := signed[:strings.Index(signed, "?")]
	path := unsigned[strings.Index(unsigned, "/dev-storage/"):]

	if r := s.doAnonymous(http.MethodGet, path, nil, nil); r.status != http.StatusForbidden {
		t.Fatalf("unsigned GET of photo bytes must be refused, got %d", r.status)
	}
	if r := s.doAnonymous(http.MethodGet, path+"?exp=9999999999&sig=deadbeef", nil, nil); r.status != http.StatusForbidden {
		t.Fatalf("forged signature must be refused, got %d", r.status)
	}
	if r := s.doAnonymous(http.MethodPut, path, map[string]string{"x": "y"}, nil); r.status != http.StatusForbidden {
		t.Fatalf("unsigned PUT must be refused, got %d", r.status)
	}
	resp, err := http.Get(signed)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signed GET must serve the bytes, got %d", resp.StatusCode)
	}
}
