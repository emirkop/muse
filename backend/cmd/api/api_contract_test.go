package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type contractCategory string

const (
	categoryIdentity    contractCategory = "identity/session/auth"
	categoryProfile     contractCategory = "profile"
	categoryMuseum      contractCategory = "museum/rooms/photos/sculptures"
	categoryCollection  contractCategory = "collection rooms/items"
	categoryCatalog     contractCategory = "catalog"
	categorySharing     contractCategory = "sharing"
	categoryMedia       contractCategory = "media/music"
	categoryEntitlement contractCategory = "entitlement/IAP"
	categoryAnalytics   contractCategory = "analytics"
	categoryOperational contractCategory = "operational (health/metrics/dev surfaces)"
)

type contract struct {
	category contractCategory
	suites   []string
}

var authorizationSweeps = []string{
	"security_gate_1_test.go",
	"security_gate_2_test.go",
	"unauthenticated_sweep_test.go",
	"owner_mutation_sweep_test.go",
}

func sweepsPlus(files ...string) []string {
	return append(append([]string{}, authorizationSweeps...), files...)
}

var contractCoverage = map[string]contract{
	"POST /auth/apple":                        {categoryIdentity, []string{"unauthenticated_sweep_test.go"}},
	"POST /auth/google":                       {categoryIdentity, []string{"unauthenticated_sweep_test.go"}},
	"POST /auth/refresh":                      {categoryIdentity, []string{"unauthenticated_sweep_test.go", "email_auth_integration_test.go"}},
	"POST /auth/logout":                       {categoryIdentity, []string{"api_contract_test.go", "unauthenticated_sweep_test.go"}},
	"POST /auth/email/signup":                 {categoryIdentity, []string{"email_auth_integration_test.go"}},
	"POST /auth/email/verify":                 {categoryIdentity, []string{"email_auth_integration_test.go"}},
	"POST /auth/email/verification/resend":    {categoryIdentity, []string{"email_auth_integration_test.go", "email_outbox_integration_test.go"}},
	"POST /auth/email/login":                  {categoryIdentity, []string{"email_auth_integration_test.go"}},
	"POST /auth/email/password-reset":         {categoryIdentity, []string{"email_auth_integration_test.go", "email_outbox_integration_test.go"}},
	"POST /auth/email/password-reset/confirm": {categoryIdentity, []string{"email_auth_integration_test.go"}},

	"GET /profile/me":          {categoryProfile, sweepsPlus("privacy_test.go")},
	"PATCH /profile/me":        {categoryProfile, sweepsPlus()},
	"GET /profile/{accountID}": {categoryProfile, sweepsPlus("public_profile_visibility_test.go", "request_hardening_test.go", "privacy_test.go")},

	"POST /museum":                                                     {categoryMuseum, sweepsPlus("photo_assignment_integration_test.go")},
	"GET /museum/me":                                                   {categoryMuseum, sweepsPlus("photo_assignment_integration_test.go")},
	"PATCH /museum/me/style":                                           {categoryMuseum, sweepsPlus()},
	"PATCH /museum/me/privacy":                                         {categoryMuseum, sweepsPlus("security_gate_2_test.go")},
	"POST /museum/me/rooms":                                            {categoryMuseum, sweepsPlus("photo_assignment_integration_test.go")},
	"GET /museum/me/rooms":                                             {categoryMuseum, sweepsPlus()},
	"GET /museum/me/rooms/{roomID}":                                    {categoryMuseum, sweepsPlus("security_gate_2_test.go")},
	"PATCH /museum/me/rooms/{roomID}":                                  {categoryMuseum, sweepsPlus("security_gate_2_test.go")},
	"DELETE /museum/me/rooms/{roomID}":                                 {categoryMuseum, sweepsPlus()},
	"POST /museum/me/rooms/{roomID}/photos":                            {categoryMuseum, sweepsPlus("photo_assignment_integration_test.go")},
	"GET /museum/me/rooms/{roomID}/photo-urls":                         {categoryMuseum, sweepsPlus("security_gate_3_test.go")},
	"PUT /museum/me/rooms/{roomID}/photo-order":                        {categoryMuseum, sweepsPlus("photo_assignment_integration_test.go")},
	"PUT /museum/me/rooms/{roomID}/photos/{photoAssetID}/caption":      {categoryMuseum, sweepsPlus()},
	"POST /museum/me/rooms/{roomID}/photos/{photoAssetID}/replacement": {categoryMuseum, sweepsPlus("photo_replacement_integration_test.go")},
	"DELETE /museum/me/rooms/{roomID}/photos/{photoAssetID}":           {categoryMuseum, sweepsPlus("photo_deletion_integration_test.go")},
	"POST /museum/me/rooms/{roomID}/sculptures":                        {categoryMuseum, sweepsPlus("sculpture_integration_test.go")},
	"DELETE /museum/me/rooms/{roomID}/sculptures/{slotIndex}":          {categoryMuseum, sweepsPlus("sculpture_integration_test.go")},
	"PUT /museum/me/rooms/{roomID}/music":                              {categoryMedia, sweepsPlus("music_integration_test.go")},
	"DELETE /museum/me/rooms/{roomID}/music":                           {categoryMedia, sweepsPlus("music_integration_test.go")},
	"GET /museums/{museumID}":                                          {categorySharing, sweepsPlus("security_gate_2_test.go", "collection_sharing_test.go")},
	"GET /museums/{museumID}/rooms/{roomID}":                           {categorySharing, sweepsPlus("security_gate_2_test.go")},

	"POST /collection-rooms":                                                 {categoryCollection, sweepsPlus("collection_integration_test.go", "collection_category_test.go")},
	"GET /collection-rooms":                                                  {categoryCollection, sweepsPlus("collection_integration_test.go")},
	"GET /collection-rooms/{collectionRoomID}":                               {categoryCollection, sweepsPlus("collection_integration_test.go")},
	"PATCH /collection-rooms/{collectionRoomID}":                             {categoryCollection, sweepsPlus("collection_integration_test.go", "collection_design_test.go")},
	"DELETE /collection-rooms/{collectionRoomID}":                            {categoryCollection, sweepsPlus("collection_integration_test.go")},
	"POST /collection-rooms/{collectionRoomID}/items":                        {categoryCollection, sweepsPlus("item_placement_test.go", "entitlement_test.go")},
	"PUT /collection-rooms/{collectionRoomID}/items/{collectionItemID}/slot": {categoryCollection, sweepsPlus("item_placement_test.go")},
	"POST /collection-rooms/{collectionRoomID}/tier":                         {categoryCollection, sweepsPlus("tier_expansion_test.go")},
	"PUT /collection-rooms/{collectionRoomID}/music":                         {categoryMedia, sweepsPlus("collection_music_test.go")},
	"DELETE /collection-rooms/{collectionRoomID}/music":                      {categoryMedia, sweepsPlus("collection_music_test.go")},

	"GET /catalog/styles":                         {categoryCatalog, []string{"api_contract_test.go", "unauthenticated_sweep_test.go"}},
	"GET /catalog/styles/{styleID}/variants":      {categoryCatalog, []string{"api_contract_test.go", "unauthenticated_sweep_test.go"}},
	"GET /catalog/room-variants/{variantID}":      {categoryCatalog, []string{"api_contract_test.go", "asset_delivery_test.go"}},
	"GET /catalog/sculptures":                     {categoryCatalog, []string{"api_contract_test.go", "sculpture_integration_test.go"}},
	"GET /catalog/music":                          {categoryCatalog, []string{"api_contract_test.go", "music_integration_test.go"}},
	"GET /catalog/music/{trackID}/audio-url":      {categoryCatalog, sweepsPlus("music_integration_test.go")},
	"GET /catalog/collection-categories":          {categoryCatalog, sweepsPlus("collection_category_test.go")},
	"GET /catalog/collection-designs":             {categoryCatalog, sweepsPlus("collection_design_test.go")},
	"GET /catalog/collection-models":              {categoryCatalog, sweepsPlus("manual_search_test.go")},
	"GET /catalog/collection-presentation-assets": {categoryCatalog, sweepsPlus("asset_mapping_test.go")},
	"GET /catalog/bundles/{bundleID}/manifest":    {categoryCatalog, sweepsPlus("asset_delivery_test.go")},

	"POST /museum/me/share-link":                                      {categorySharing, sweepsPlus("sharing_integration_test.go")},
	"GET /museum/me/share-link":                                       {categorySharing, sweepsPlus("sharing_integration_test.go")},
	"POST /museum/me/share-link/regenerate":                           {categorySharing, sweepsPlus("sharing_integration_test.go")},
	"GET /share-links/{code}":                                         {categorySharing, []string{"sharing_integration_test.go", "unauthenticated_sweep_test.go"}},
	"GET /share-links/{code}/museum":                                  {categorySharing, sweepsPlus("visitor_integration_test.go", "concurrent_visitors_test.go")},
	"GET /share-links/{code}/rooms/{roomID}":                          {categorySharing, sweepsPlus("visitor_integration_test.go")},
	"GET /share-links/{code}/rooms/{roomID}/photo-urls":               {categorySharing, sweepsPlus("visitor_integration_test.go", "security_gate_3_test.go")},
	"POST /collection-rooms/{collectionRoomID}/share-link":            {categorySharing, sweepsPlus("collection_sharing_test.go")},
	"GET /collection-rooms/{collectionRoomID}/share-link":             {categorySharing, sweepsPlus("collection_sharing_test.go")},
	"DELETE /collection-rooms/{collectionRoomID}/share-link":          {categorySharing, sweepsPlus("collection_sharing_test.go")},
	"POST /collection-rooms/{collectionRoomID}/share-link/regenerate": {categorySharing, sweepsPlus("collection_sharing_test.go")},
	"GET /collection-share-links/{code}/collection-room":              {categorySharing, sweepsPlus("collection_sharing_test.go")},
	"GET /m/{code}": {categorySharing, []string{"sharing_integration_test.go", "unauthenticated_sweep_test.go", "request_hardening_test.go"}},
	"GET /c/{code}": {categorySharing, []string{"collection_sharing_test.go", "unauthenticated_sweep_test.go", "request_hardening_test.go"}},
	"GET /.well-known/apple-app-site-association": {categorySharing, []string{"sharing_integration_test.go", "unauthenticated_sweep_test.go"}},

	"POST /media/photo-uploads": {categoryMedia, sweepsPlus("photo_assignment_integration_test.go")},

	"GET /entitlements/me":                      {categoryEntitlement, sweepsPlus("entitlement_test.go")},
	"POST /entitlements/app-account-token":      {categoryEntitlement, sweepsPlus("entitlement_test.go")},
	"POST /entitlements/app-store/transactions": {categoryEntitlement, sweepsPlus("entitlement_test.go", "verification_boundary_test.go")},
	"POST /app-store/notifications":             {categoryEntitlement, []string{"entitlement_test.go", "verification_boundary_test.go", "unauthenticated_sweep_test.go"}},

	"POST /analytics/events": {categoryAnalytics, sweepsPlus("analytics_test.go")},

	"GET /health":               {categoryOperational, []string{"unauthenticated_sweep_test.go", "monitoring_test.go"}},
	"GET /health/ready":         {categoryOperational, []string{"unauthenticated_sweep_test.go", "monitoring_test.go"}},
	"GET /metrics":              {categoryOperational, []string{"unauthenticated_sweep_test.go", "monitoring_test.go"}},
	"PUT /dev-storage/{key...}": {categoryOperational, []string{"unauthenticated_sweep_test.go", "security_gate_3_test.go"}},
	"GET /dev-storage/{key...}": {categoryOperational, []string{"unauthenticated_sweep_test.go", "security_gate_3_test.go"}},
	"GET /dev-assets/{key...}":  {categoryOperational, []string{"unauthenticated_sweep_test.go", "security_gate_3_test.go", "asset_delivery_test.go"}},
}

// MARK: - The classification, machine-checked

var bodylessMutations = map[string]bool{
	"POST /museum/me/share-link":                                      true,
	"POST /museum/me/share-link/regenerate":                           true,
	"POST /collection-rooms/{collectionRoomID}/share-link":            true,
	"POST /collection-rooms/{collectionRoomID}/share-link/regenerate": true,
	"POST /entitlements/app-account-token":                            true,
}

func TestEveryRouteHasADeclaredContract(t *testing.T) {
	routes := routeInventory(t)

	var undeclared []string
	for _, route := range routes {
		if _, ok := contractCoverage[route]; !ok {
			undeclared = append(undeclared, route)
		}
	}
	if len(undeclared) > 0 {
		sort.Strings(undeclared)
		t.Errorf("these routes have no declared contract coverage — add them to contractCoverage "+
			"with the category and the suite(s) that exercise them:\n  %s",
			strings.Join(undeclared, "\n  "))
	}

	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[route] = true
	}
	var stale []string
	for route := range contractCoverage {
		if !registered[route] {
			stale = append(stale, route)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("these declared contracts name routes that are no longer registered:\n  %s",
			strings.Join(stale, "\n  "))
	}

	if len(routes) != len(contractCoverage) {
		t.Errorf("%d routes registered, %d contracts declared", len(routes), len(contractCoverage))
	}
}

func TestEveryDeclaredContractSuiteExists(t *testing.T) {
	present := map[string]bool{}
	matches, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		present[m] = true
	}
	if len(present) == 0 {
		t.Fatal("found no test files in cmd/api — the check is broken, not the suite")
	}

	missing := map[string][]string{}
	for route, c := range contractCoverage {
		if len(c.suites) == 0 {
			t.Errorf("%s declares no suite", route)
		}
		for _, suite := range c.suites {
			if !present[suite] {
				missing[suite] = append(missing[suite], route)
			}
		}
	}
	for suite, routes := range missing {
		sort.Strings(routes)
		t.Errorf("declared contract suite %q does not exist; claimed by: %s", suite, strings.Join(routes, ", "))
	}
}

func TestEveryContractCategoryAndSweepIsPresent(t *testing.T) {
	byCategory := map[contractCategory]int{}
	for _, c := range contractCoverage {
		byCategory[c.category]++
	}
	for _, category := range []contractCategory{
		categoryIdentity, categoryProfile, categoryMuseum, categoryCollection,
		categoryCatalog, categorySharing, categoryMedia, categoryEntitlement,
		categoryAnalytics, categoryOperational,
	} {
		if byCategory[category] == 0 {
			t.Errorf("contract category %q covers no route", category)
		}
	}

	for _, sweep := range authorizationSweeps {
		if _, err := os.Stat(sweep); err != nil {
			t.Errorf("authorization sweep %q is gone: %v — requires these as permanent coverage", sweep, err)
		}
	}

	claimed := map[string]bool{}
	for _, c := range contractCoverage {
		for _, suite := range c.suites {
			claimed[suite] = true
		}
	}
	for _, sweep := range authorizationSweeps {
		if !claimed[sweep] {
			t.Errorf("authorization sweep %q is on disk but no contract delegates to it", sweep)
		}
	}
}

// MARK: - Gap 1: logout must actually end the session

func TestLogout_InvalidatesTheSession(t *testing.T) {
	s := newEmailAuthStack(t)
	session := s.signUpAndVerify(t, "logout-contract@example.com", "correct horse battery staple")
	refreshToken, _ := session["refresh_token"].(string)
	if refreshToken == "" {
		t.Fatal("precondition: verification should return a session")
	}

	status, refreshed := s.post(t, "/auth/refresh", map[string]string{"refresh_token": refreshToken})
	if status != http.StatusOK {
		t.Fatalf("precondition: refresh before logout → %d %v", status, refreshed)
	}
	live, _ := refreshed["refresh_token"].(string)
	if live == "" {
		t.Fatal("precondition: refresh should return a rotated token")
	}

	if status, body := s.post(t, "/auth/logout", map[string]string{"refresh_token": live}); status != http.StatusOK {
		t.Fatalf("logout → %d %v, want 200", status, body)
	}

	if status, body := s.post(t, "/auth/refresh", map[string]string{"refresh_token": live}); status != http.StatusUnauthorized {
		t.Fatalf("refresh after logout → %d %v, want 401 — logout must end the session, not just answer 200",
			status, body)
	}

	for _, token := range []string{live, "never-a-real-token"} {
		if status, body := s.post(t, "/auth/logout", map[string]string{"refresh_token": token}); status != http.StatusOK {
			t.Errorf("repeat logout → %d %v, want 200 (forgiving by contract)", status, body)
		}
	}

	if status, _ := s.post(t, "/auth/logout", map[string]string{}); status != http.StatusBadRequest {
		t.Errorf("logout with no refresh_token → %d, want 400", status)
	}
}

// MARK: - Gap 2: the catalog reads' response shape

func TestCatalogReads_HaveAStableResponseShape(t *testing.T) {
	s := newStack(t)

	t.Run("styles", func(t *testing.T) {
		resp, body := s.do(http.MethodGet, "/catalog/styles", nil, s.token)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("→ %d %s", resp.StatusCode, body)
		}
		envelope := decodeObject(t, body)
		list := objectsUnder(t, envelope, "styles")
		if len(list) == 0 {
			t.Fatal("no styles served; the seeded catalog should have them")
		}
		for _, style := range list {
			assertExactKeys(t, style, "id", "display_name", "asset_bundle_id", "asset_bundle_version")
		}
	})

	t.Run("variants for a style", func(t *testing.T) {
		styles := objectsUnder(t, decodeObject(t, mustGet(t, s, "/catalog/styles")), "styles")
		styleID, _ := styles[0]["id"].(string)
		if styleID == "" {
			t.Fatal("a style with no id cannot be asked for variants")
		}

		resp, body := s.do(http.MethodGet, "/catalog/styles/"+styleID+"/variants", nil, s.token)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("→ %d %s", resp.StatusCode, body)
		}
		list := objectsUnder(t, decodeObject(t, body), "variants")
		if len(list) == 0 {
			t.Fatal("no variants served for a seeded style")
		}
		for _, variant := range list {
			assertExactKeys(t, variant, "id", "style_id", "display_name", "asset_bundle_id", "asset_bundle_version")
			if variant["style_id"] != styleID {
				t.Errorf("variant %v is scoped to %v, not the requested %q", variant["id"], variant["style_id"], styleID)
			}
		}

		unknown, unknownBody := s.do(http.MethodGet, "/catalog/styles/style_does_not_exist/variants", nil, s.token)
		if unknown.StatusCode != http.StatusOK {
			t.Fatalf("unknown style → %d %s, want 200 with an empty list", unknown.StatusCode, unknownBody)
		}
		if got := objectsUnder(t, decodeObject(t, unknownBody), "variants"); len(got) != 0 {
			t.Errorf("unknown style served %d variants", len(got))
		}
	})

	t.Run("sculptures ship empty", func(t *testing.T) {
		envelope := decodeObject(t, mustGet(t, s, "/catalog/sculptures"))
		raw, present := envelope["sculptures"]
		if !present {
			t.Fatal("the response must carry a `sculptures` key even when empty")
		}
		if raw == nil {
			t.Fatal("`sculptures` must be an empty array, not null — a client decodes an array")
		}
		for _, sculpture := range objectsUnder(t, envelope, "sculptures") {
			assertExactKeys(t, sculpture, "id", "display_name", "asset_bundle_id", "asset_bundle_version")
		}
	})

	t.Run("music ships empty", func(t *testing.T) {
		envelope := decodeObject(t, mustGet(t, s, "/catalog/music"))
		raw, present := envelope["tracks"]
		if !present {
			t.Fatal("the response must carry a `tracks` key even when empty")
		}
		if raw == nil {
			t.Fatal("`tracks` must be an empty array, not null")
		}
		for _, track := range objectsUnder(t, envelope, "tracks") {
			assertExactKeys(t, track, "id", "display_name", "attribution", "licensing", "duration_seconds")
		}
	})
}

// MARK: - Gap 3: malformed input, systematically

func TestMalformedJSONBodies_AreRefusedEverywhere(t *testing.T) {
	s := newStack(t)
	room := s.createRoom()
	collectionRoom := s.createCollectionRoomInCategory(s.token, "Contract", seededCategory)

	fill := strings.NewReplacer(
		"{roomID}", room,
		"{collectionRoomID}", collectionRoom,
		"{photoAssetID}", bogusUUID,
		"{collectionItemID}", bogusUUID,
		"{slotIndex}", "0",
		"{accountID}", s.accountID,
		"{museumID}", bogusUUID,
		"{code}", "aaaaaaaaaaaaaaaaaaaaaa",
		"{styleID}", "style_modern",
		"{variantID}", "style_modern_variant_Hall",
		"{trackID}", "track_dev_a",
		"{bundleID}", "bundle_style_modern",
	)

	before := s.snapshotOwnerState()
	swept, bodyless := 0, 0
	for route := range contractCoverage {
		method, path, ok := strings.Cut(route, " ")
		if !ok {
			t.Fatalf("malformed route in the ledger: %q", route)
		}
		switch method {
		case http.MethodGet, http.MethodDelete:
			continue
		}
		if strings.Contains(path, "{key...}") || path == "/app-store/notifications" {
			continue
		}

		resolved := fill.Replace(path)
		resp, body := s.doRaw(method, resolved, []byte("{this is not json"), s.token)

		if bodylessMutations[route] {
			bodyless++
			if resp.StatusCode >= 500 {
				t.Errorf("%s %s (bodyless) with a malformed body → %d %s; an unread body must not be fatal",
					method, resolved, resp.StatusCode, body)
			}
			continue
		}

		swept++
		switch resp.StatusCode {
		case http.StatusBadRequest:
		case http.StatusUnauthorized, http.StatusServiceUnavailable:
		case http.StatusNotFound:
		default:
			t.Errorf("%s %s with a malformed body → %d %s; want 400 (or an earlier refusal)",
				method, resolved, resp.StatusCode, body)
		}
	}

	if bodyless == 0 {
		t.Error("no bodyless route was exercised — `bodylessMutations` is not reaching the sweep")
	}
	t.Logf("malformed-body sweep: %d body-reading routes refused, %d bodyless routes ignored it", swept, bodyless)
	if swept < 20 {
		t.Fatalf("only %d routes swept — the ledger-derived selection is not working", swept)
	}
	if after := s.snapshotOwnerState(); after != before {
		t.Errorf("malformed bodies changed state:\n  before %+v\n  after  %+v", before, after)
	}
}

// MARK: - Helpers

func mustGet(t *testing.T, s *stack, path string) []byte {
	t.Helper()
	resp, body := s.do(http.MethodGet, path, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s → %d %s", path, resp.StatusCode, body)
	}
	return body
}

func decodeObject(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, body)
	}
	return out
}

func objectsUnder(t *testing.T, envelope map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := envelope[key]
	if !ok {
		t.Fatalf("response has no %q key; keys: %v", key, sortedKeysOf(envelope))
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("%q is %T, want an array", key, raw)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("an element of %q is %T, want an object", key, item)
		}
		out = append(out, object)
	}
	return out
}

func assertExactKeys(t *testing.T, object map[string]any, keys ...string) {
	t.Helper()
	want := make(map[string]bool, len(keys))
	for _, key := range keys {
		want[key] = true
	}
	for key := range object {
		if !want[key] {
			t.Errorf("unexpected key %q in %v — a new field is a contract change", key, sortedKeysOf(object))
		}
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			t.Errorf("missing key %q in %v", key, sortedKeysOf(object))
		}
	}
}

func sortedKeysOf(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *stack) doRaw(method, path string, body []byte, token string) (*http.Response, []byte) {
	s.t.Helper()
	request, err := http.NewRequest(method, s.server.URL+path, bytes.NewReader(body))
	if err != nil {
		s.t.Fatalf("request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		s.t.Fatalf("do: %v", err)
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		s.t.Fatalf("read: %v", err)
	}
	response.Body.Close()
	return response, raw
}
