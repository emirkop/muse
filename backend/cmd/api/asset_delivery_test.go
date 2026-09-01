package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	catalogapp "muse-backend/internal/catalog/application"
	catalogdomain "muse-backend/internal/catalog/domain"
	mediainfra "muse-backend/internal/media/infrastructure"
)

// MARK: - Fixture helpers

type publishedBundle struct {
	bundleID string
	version  int
	bytes    map[string][]byte
}

func (s *stack) publishFixtureBundle(t *testing.T, bundleID string, version int, geometry string, opts ...func(*catalogapp.PublishRequest)) publishedBundle {
	t.Helper()

	layout := fmt.Sprintf(`{"variant_id":"dev-fixture:room-variant","format_version":1,"version_marker":%d}`, version)
	request := catalogapp.PublishRequest{
		BundleID:      bundleID,
		Version:       version,
		Kind:          catalogdomain.BundleKindRoomVariant,
		Format:        "usda",
		MinAppVersion: 1,
		Files: []catalogapp.PublishSource{
			newPublishSource("geometry", catalogdomain.RoleGeometry, "model/vnd.usda+ascii", geometry),
			newPublishSource("layout", catalogdomain.RoleLayout, "application/json", layout),
		},
	}
	for _, apply := range opts {
		apply(&request)
	}

	result, err := s.publisher.Publish(context.Background(), request)
	if err != nil {
		t.Fatalf("publish %s v%d: %v", bundleID, version, err)
	}
	if result.AlreadyPublished {
		t.Fatalf("publish %s v%d unexpectedly reported already-published", bundleID, version)
	}
	return publishedBundle{
		bundleID: bundleID,
		version:  version,
		bytes:    map[string][]byte{"geometry": []byte(geometry), "layout": []byte(layout)},
	}
}

func newPublishSource(assetID string, role catalogdomain.AssetRole, contentType, body string) catalogapp.PublishSource {
	sum := sha256.Sum256([]byte(body))
	return catalogapp.PublishSource{
		AssetID:        assetID,
		Role:           role,
		ContentType:    contentType,
		ByteSize:       int64(len(body)),
		ChecksumSHA256: hex.EncodeToString(sum[:]),
		Open: func() (catalogapp.ReadSeekCloser, error) {
			return readSeekCloser{strings.NewReader(body)}, nil
		},
	}
}

type readSeekCloser struct{ *strings.Reader }

func (readSeekCloser) Close() error { return nil }

type manifestBody struct {
	BundleID      string `json:"bundle_id"`
	Version       int    `json:"version"`
	Kind          string `json:"kind"`
	Format        string `json:"format"`
	MinAppVersion int    `json:"min_app_version"`
	Files         []struct {
		AssetID        string `json:"asset_id"`
		Role           string `json:"role"`
		URL            string `json:"url"`
		ContentType    string `json:"content_type"`
		ByteSize       int64  `json:"byte_size"`
		ChecksumSHA256 string `json:"checksum_sha256"`
	} `json:"files"`
	Dependencies []struct {
		BundleID string `json:"bundle_id"`
		Version  int    `json:"version"`
	} `json:"dependencies"`
}

func (s *stack) fetchManifest(t *testing.T, bundleID string, query string) (*http.Response, manifestBody) {
	t.Helper()
	path := "/catalog/bundles/" + bundleID + "/manifest" + query
	resp, raw := s.do(http.MethodGet, path, nil, s.token)
	var body manifestBody
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode manifest: %v (%s)", err, raw)
		}
	}
	return resp, body
}

// MARK: - PROOF 1: publish, then download

func TestPublishedBundleIsFetchableThroughItsManifest(t *testing.T) {
	s := newStack(t)
	published := s.publishFixtureBundle(t, "dev_fixture_room_variant", 1, devFixtureGeometry(1))

	resp, manifest := s.fetchManifest(t, published.bundleID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest: expected 200, got %d", resp.StatusCode)
	}
	if manifest.Version != 1 || manifest.Kind != "room_variant" || manifest.Format != "usda" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("manifest Cache-Control = %q, want no-store", got)
	}
	if len(manifest.Files) != 2 || manifest.Files[0].Role != "geometry" {
		t.Fatalf("expected geometry first in a 2-file bundle, got %+v", manifest.Files)
	}

	for _, file := range manifest.Files {
		expected := published.bytes[file.AssetID]
		body, headers := fetchURL(t, file.URL, nil)
		if string(body) != string(expected) {
			t.Errorf("%s: downloaded %d bytes, expected %d", file.AssetID, len(body), len(expected))
		}
		if file.ByteSize != int64(len(expected)) {
			t.Errorf("%s: manifest says %d bytes, file is %d", file.AssetID, file.ByteSize, len(expected))
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != file.ChecksumSHA256 {
			t.Errorf("%s: downloaded bytes do not match the manifest checksum", file.AssetID)
		}
		if cache := headers.Get("Cache-Control"); !strings.Contains(cache, "immutable") {
			t.Errorf("%s: bundle bytes Cache-Control = %q, want an immutable directive", file.AssetID, cache)
		}
	}
}

func TestUnpublishedBundleIsNotFound(t *testing.T) {
	s := newStack(t)
	resp, _ := s.fetchManifest(t, "bundle_style_modern", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for an unpublished bundle, got %d", resp.StatusCode)
	}
}

// MARK: - PROOF 2 (server half): the byte URL honours HTTP Range

func TestBundleBytesSupportRangeRequests(t *testing.T) {
	s := newStack(t)
	geometry := devFixtureGeometry(1)
	published := s.publishFixtureBundle(t, "dev_fixture_room_variant", 1, geometry)

	_, manifest := s.fetchManifest(t, published.bundleID, "")
	var geometryURL string
	for _, file := range manifest.Files {
		if file.Role == "geometry" {
			geometryURL = file.URL
		}
	}
	if geometryURL == "" {
		t.Fatal("no geometry file in the manifest")
	}

	total := len(geometry)
	offset := total / 3

	req, _ := http.NewRequest(http.MethodGet, geometryURL, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	resp, err := testHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("range request: %v", err)
	}
	defer resp.Body.Close()
	tail, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d — a client cannot resume against this", resp.StatusCode)
	}
	wantRange := fmt.Sprintf("bytes %d-%d/%d", offset, total-1, total)
	if got := resp.Header.Get("Content-Range"); got != wantRange {
		t.Errorf("Content-Range = %q, want %q", got, wantRange)
	}
	if string(tail) != geometry[offset:] {
		t.Errorf("range body is not the expected tail (%d bytes vs %d)", len(tail), total-offset)
	}
	if geometry[:offset]+string(tail) != geometry {
		t.Error("prefix + resumed tail does not reconstruct the original file")
	}

	req, _ = http.NewRequest(http.MethodGet, geometryURL, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", total+10))
	resp2, err := testHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("unsatisfiable range request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("expected 416 for an out-of-range offset, got %d", resp2.StatusCode)
	}
}

// MARK: - PROOF 4/5 (server half): versions supersede, and stale cannot pass as current

func TestPublishingV2MakesV1Stale(t *testing.T) {
	s := newStack(t)
	const bundleID = "dev_fixture_room_variant"
	v1 := s.publishFixtureBundle(t, bundleID, 1, devFixtureGeometry(1))

	_, first := s.fetchManifest(t, bundleID, "")
	if first.Version != 1 {
		t.Fatalf("expected v1 current, got v%d", first.Version)
	}
	v1GeometryURL := urlForRole(t, first, "geometry")

	v2 := s.publishFixtureBundle(t, bundleID, 2, devFixtureGeometry(2))

	_, second := s.fetchManifest(t, bundleID, "")
	if second.Version != 2 {
		t.Fatalf("expected v2 current after publishing it, got v%d", second.Version)
	}
	v2GeometryURL := urlForRole(t, second, "geometry")

	if v1GeometryURL == v2GeometryURL {
		t.Fatal("v1 and v2 share a byte URL — versions are not key-isolated, so a cached v1 could be served as v2")
	}
	v1Body, _ := fetchURL(t, v1GeometryURL, nil)
	if string(v1Body) != string(v1.bytes["geometry"]) {
		t.Error("v1's bytes changed when v2 was published")
	}
	v2Body, _ := fetchURL(t, v2GeometryURL, nil)
	if string(v2Body) != string(v2.bytes["geometry"]) {
		t.Error("v2's URL does not serve v2's bytes")
	}

	var reported int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT asset_bundle_version FROM room_variants WHERE asset_bundle_id = $1 LIMIT 1`,
		bundleID).Scan(&reported); err == nil && reported != 2 {
		t.Errorf("a referencing Variant still reports v%d", reported)
	}
}

func TestNoRequestShapeReturnsASupersededVersionAsCurrent(t *testing.T) {
	s := newStack(t)
	const bundleID = "dev_fixture_room_variant"
	s.publishFixtureBundle(t, bundleID, 1, devFixtureGeometry(1))
	s.publishFixtureBundle(t, bundleID, 2, devFixtureGeometry(2))

	for _, query := range []string{
		"",
		"?app_asset_version=1",
		"?app_asset_version=2",
		"?app_asset_version=999",
		"?version=1",
		"?bundle_version=1",
	} {
		resp, manifest := s.fetchManifest(t, bundleID, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%q: expected 200, got %d", query, resp.StatusCode)
		}
		if manifest.Version != 2 {
			t.Errorf("%q returned v%d as current — v1 is stale and must be unreachable as current", query, manifest.Version)
		}
	}

	for _, query := range []string{"?app_asset_version=0", "?app_asset_version=-1", "?app_asset_version=abc", "?app_asset_version=99999"} {
		resp, _ := s.fetchManifest(t, bundleID, query)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: expected 400, got %d", query, resp.StatusCode)
		}
	}
}

func TestAnOldClientIsServedTheNewestCompatibleVersion(t *testing.T) {
	s := newStack(t)
	const bundleID = "dev_fixture_room_variant"
	s.publishFixtureBundle(t, bundleID, 1, devFixtureGeometry(1))
	s.publishFixtureBundle(t, bundleID, 2, devFixtureGeometry(2), func(r *catalogapp.PublishRequest) {
		r.MinAppVersion = 2
		r.Format = "usdz"
	})

	_, old := s.fetchManifest(t, bundleID, "?app_asset_version=1")
	if old.Version != 1 || old.Format != "usda" {
		t.Errorf("an app at asset version 1 must receive v1/usda, got v%d/%s", old.Version, old.Format)
	}
	_, updated := s.fetchManifest(t, bundleID, "?app_asset_version=2")
	if updated.Version != 2 || updated.Format != "usdz" {
		t.Errorf("an app at asset version 2 must receive v2/usdz, got v%d/%s", updated.Version, updated.Format)
	}
}

// MARK: - PROOF 6: asset identity is separate from Museum content

func TestBundleTablesReferenceNoUserContent(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	const forbidden = `
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_name IN ('asset_bundles', 'asset_bundle_files', 'asset_bundle_dependencies')
		  AND (column_name LIKE '%account%'
		    OR column_name LIKE '%museum%'
		    OR column_name LIKE '%room%'
		    OR column_name LIKE '%photo%'
		    OR column_name LIKE '%user%')
	`
	rows, err := s.pool.Pool().Query(ctx, forbidden)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s.%s ties asset identity to user content — the two must stay structurally separate (04's highest named risk)", table, column)
	}

	const referencesFromContent = `
		SELECT c.table_name, c.column_name
		FROM information_schema.columns c
		WHERE c.table_name IN ('museums', 'rooms', 'room_photo_slots', 'room_sculptures', 'assets')
		  AND c.column_name LIKE '%bundle%'
	`
	contentRows, err := s.pool.Pool().Query(ctx, referencesFromContent)
	if err != nil {
		t.Fatalf("introspect content: %v", err)
	}
	defer contentRows.Close()
	for contentRows.Next() {
		var table, column string
		if err := contentRows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		t.Errorf("content table %s carries %s — a bundle reference belongs on a presentation row only", table, column)
	}
}

func TestPublishingAVersionDoesNotTouchUserContent(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	roomID := s.createRoom()
	photo := s.uploaded(newPhoto(t, 640, 480, "delivery-content"))
	if resp, _, errBody := s.assign(roomID, []string{photo.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign: %d %v", resp.StatusCode, errBody)
	}
	before := s.contentSnapshot(t)

	s.publishFixtureBundle(t, "dev_fixture_room_variant", 1, devFixtureGeometry(1))
	s.publishFixtureBundle(t, "dev_fixture_room_variant", 2, devFixtureGeometry(2))
	s.publishFixtureBundle(t, seededVariantBundleID(t, s), 1, devFixtureGeometry(1))

	after := s.contentSnapshot(t)
	if before != after {
		t.Errorf("publishing asset bundles changed user content:\nbefore %s\nafter  %s", before, after)
	}
	_ = ctx
}

func (s *stack) contentSnapshot(t *testing.T) string {
	t.Helper()
	const query = `
		SELECT COALESCE(json_agg(row_to_json(t) ORDER BY t.kind, t.id)::text, '[]')
		FROM (
			SELECT 'museum' AS kind, id::text AS id, style_id AS a, privacy AS b, '' AS c FROM museums
			UNION ALL
			SELECT 'room', id::text, name, variant_id, privacy FROM rooms
			UNION ALL
			SELECT 'slot', id::text, slot_index::text, photo_asset_id::text, caption FROM room_photo_slots
			UNION ALL
			SELECT 'sculpture', id::text, slot_index::text, catalog_id, '' FROM room_sculptures
		) t
	`
	var snapshot string
	if err := s.pool.Pool().QueryRow(context.Background(), query).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snapshot
}

func seededVariantBundleID(t *testing.T, s *stack) string {
	t.Helper()
	var bundleID string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT asset_bundle_id FROM room_variants ORDER BY id LIMIT 1`).Scan(&bundleID); err != nil {
		t.Fatalf("seeded variant: %v", err)
	}
	return bundleID
}

// MARK: - PROOF 7: the fixture is swappable for real art with no code change

func TestPublishingUnderASeededVariantNeedsNoDeliveryChange(t *testing.T) {
	s := newStack(t)

	bundleID := seededVariantBundleID(t, s)
	resp, _ := s.fetchManifest(t, bundleID, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a seeded Variant with no published bundle must be 404, got %d", resp.StatusCode)
	}

	s.publishFixtureBundle(t, bundleID, 3, devFixtureGeometry(3), func(r *catalogapp.PublishRequest) {
		r.Format = "usdz"
	})

	resp, manifest := s.fetchManifest(t, bundleID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after publishing, got %d", resp.StatusCode)
	}
	if manifest.Format != "usdz" || manifest.Version != 3 {
		t.Errorf("unexpected manifest: v%d %s", manifest.Version, manifest.Format)
	}
	variantResp, variantRaw := s.do(http.MethodGet, "/catalog/room-variants/"+seededVariantID(t, s), nil, s.token)
	if variantResp.StatusCode != http.StatusOK {
		t.Fatalf("variant lookup: expected 200, got %d (%s)", variantResp.StatusCode, variantRaw)
	}
	var variant struct {
		AssetBundleID      string `json:"asset_bundle_id"`
		AssetBundleVersion int    `json:"asset_bundle_version"`
	}
	if err := json.Unmarshal(variantRaw, &variant); err != nil {
		t.Fatalf("decode variant: %v", err)
	}
	if variant.AssetBundleID != bundleID || variant.AssetBundleVersion != 3 {
		t.Errorf("variant reports %s v%d, expected %s v3", variant.AssetBundleID, variant.AssetBundleVersion, bundleID)
	}
}

func seededVariantID(t *testing.T, s *stack) string {
	t.Helper()
	var variantID string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT id FROM room_variants ORDER BY id LIMIT 1`).Scan(&variantID); err != nil {
		t.Fatalf("seeded variant id: %v", err)
	}
	return variantID
}

func TestUnknownVariantLookupIsNotFound(t *testing.T) {
	s := newStack(t)
	resp, _ := s.do(http.MethodGet, "/catalog/room-variants/not-a-variant", nil, s.token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// MARK: - The public byte surface reaches bundles and nothing else

func TestPublicAssetSurfaceCannotReachAPhotograph(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	photo := s.uploaded(newPhoto(t, 640, 480, "delivery-private"))
	if resp, _, errBody := s.assign(roomID, []string{photo.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign: %d %v", resp.StatusCode, errBody)
	}

	photoKey := fmt.Sprintf("photos/%s/%s", s.accountID, photo.asset)
	resp, _ := fetchURLResponse(t, s.server.URL+mediainfra.DevPublicAssetPathPrefix+photoKey)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a photograph key must be unreachable through the public asset surface, got %d", resp.StatusCode)
	}

	for _, key := range []string{
		"../photos/" + s.accountID + "/" + photo.asset,
		"photos/../bundles/x",
		"",
	} {
		resp, _ := fetchURLResponse(t, s.server.URL+mediainfra.DevPublicAssetPathPrefix+key)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("key %q returned %d, expected 404", key, resp.StatusCode)
		}
	}

	published := s.publishFixtureBundle(t, "dev_fixture_room_variant", 1, devFixtureGeometry(1))
	body, _ := fetchURL(t, s.bundleStore.PublicURL(catalogdomain.StorageKeyFor(published.bundleID, 1, "geometry")), nil)
	if string(body) != string(published.bytes["geometry"]) {
		t.Error("a published bundle file is not served through the public asset surface")
	}
}

// MARK: - helpers

func urlForRole(t *testing.T, manifest manifestBody, role string) string {
	t.Helper()
	for _, file := range manifest.Files {
		if file.Role == role {
			return file.URL
		}
	}
	t.Fatalf("no %s file in manifest", role)
	return ""
}

func fetchURL(t *testing.T, url string, headers map[string]string) ([]byte, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := testHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("GET %s: %d (%s)", url, resp.StatusCode, body)
	}
	return body, resp.Header
}

func fetchURLResponse(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := testGet(url) //nolint:gosec // a test URL built from the test server
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

func devFixtureGeometry(version int) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "#usda 1.0\n(\n    doc = \"MUSE DEVELOPMENT FIXTURE v%d - NOT production artwork\"\n)\n", version)
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&builder, "# fixture v%d filler line %03d\n", version, i)
	}
	return builder.String()
}
