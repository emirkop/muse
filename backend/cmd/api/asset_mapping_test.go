package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	catalogapp "muse-backend/internal/catalog/application"
	catalogdomain "muse-backend/internal/catalog/domain"
	catalinfra "muse-backend/internal/catalog/infrastructure"
)

// MARK: - Harness

type presentationAssetJSON struct {
	ModelID              string `json:"model_id"`
	HasPresentationAsset bool   `json:"has_presentation_asset"`
	AssetBundleID        string `json:"asset_bundle_id"`
	AssetBundleVersion   int    `json:"asset_bundle_version"`
	IsDevelopmentFixture bool   `json:"is_development_fixture"`
}

type presentationAssetsJSON struct {
	Assets []presentationAssetJSON `json:"assets"`
}

func (s *stack) presentationAssets(modelIDs ...string) (*http.Response, presentationAssetsJSON) {
	s.t.Helper()
	path := "/catalog/collection-presentation-assets?model_ids=" + strings.Join(modelIDs, ",")
	resp, raw := s.do(http.MethodGet, path, nil, s.token)
	var body presentationAssetsJSON
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &body); err != nil {
			s.t.Fatalf("decode presentation assets: %v (%s)", err, raw)
		}
	}
	return resp, body
}

func (body presentationAssetsJSON) byModel() map[string]presentationAssetJSON {
	keyed := map[string]presentationAssetJSON{}
	for _, asset := range body.Assets {
		keyed[asset.ModelID] = asset
	}
	return keyed
}

const (
	mappedModel     = "dev-fixture:model-chrono-one"
	unmappedModel   = "dev-fixture:model-chrono-two"
	unmappedModel2  = "dev-fixture:model-diver"
	otherCategory   = "dev-fixture:model-racer"
	collectionModel = "dev_fixture_collection_model"
)

func committedModelFixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "..", "assets", "dev_fixtures", "bundles",
		collectionModel, "v1")
}

func (s *stack) publishCommittedModelFixture(t *testing.T) catalogapp.PublishResult {
	t.Helper()
	dir := committedModelFixtureDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		t.Fatalf("read committed bundle.json: %v", err)
	}
	var spec struct {
		BundleID      string `json:"bundle_id"`
		Version       int    `json:"version"`
		Kind          string `json:"kind"`
		Format        string `json:"format"`
		MinAppVersion int    `json:"min_app_version"`
		Files         []struct {
			AssetID     string `json:"asset_id"`
			Role        string `json:"role"`
			Path        string `json:"path"`
			ContentType string `json:"content_type"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse committed bundle.json: %v", err)
	}
	request := catalogapp.PublishRequest{
		BundleID: spec.BundleID, Version: spec.Version,
		Kind: catalogdomain.BundleKind(spec.Kind), Format: spec.Format,
		MinAppVersion: spec.MinAppVersion,
	}
	for _, file := range spec.Files {
		body, err := os.ReadFile(filepath.Join(dir, file.Path))
		if err != nil {
			t.Fatalf("read committed %s: %v", file.Path, err)
		}
		request.Files = append(request.Files,
			newPublishSource(file.AssetID, catalogdomain.AssetRole(file.Role), file.ContentType, string(body)))
	}
	result, err := s.publisher.Publish(context.Background(), request)
	if err != nil {
		t.Fatalf("publish the committed Model fixture: %v", err)
	}
	return result
}

// MARK: - The mapping's three states

func TestTheMappingHasThreeDistinguishableStates(t *testing.T) {
	s := newStack(t)

	resp, body := s.presentationAssets(mappedModel, unmappedModel, "dev-fixture:model-does-not-exist")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presentation assets: %d", resp.StatusCode)
	}
	keyed := body.byModel()

	mapped, present := keyed[mappedModel]
	if !present {
		t.Fatalf("the mapped Model is absent from %v", keyed)
	}
	if !mapped.HasPresentationAsset {
		t.Fatal("the mapped Model reports no presentation asset")
	}
	if mapped.AssetBundleID != collectionModel || mapped.AssetBundleVersion != 1 {
		t.Fatalf("mapped to %s v%d, want %s v1", mapped.AssetBundleID, mapped.AssetBundleVersion, collectionModel)
	}

	unmapped, present := keyed[unmappedModel]
	if !present {
		t.Fatal("a Model with no asset must still be reported, not omitted")
	}
	if unmapped.HasPresentationAsset {
		t.Fatal("a Model with no asset reports one")
	}
	if unmapped.AssetBundleID != "" || unmapped.AssetBundleVersion != 0 {
		t.Fatalf("an unmapped Model carried a bundle reference: %+v", unmapped)
	}

	if _, present := keyed["dev-fixture:model-does-not-exist"]; present {
		t.Fatal("an unknown Model id produced an entry")
	}
	if len(body.Assets) != 2 {
		t.Fatalf("%d entries for 3 requested ids, want 2", len(body.Assets))
	}
}

func TestTheLookupIsBatchAndDeduplicates(t *testing.T) {
	s := newStack(t)

	_, body := s.presentationAssets(mappedModel, unmappedModel, mappedModel, unmappedModel2, mappedModel)

	if len(body.Assets) != 3 {
		t.Fatalf("%d entries, want 3 distinct", len(body.Assets))
	}
	keyed := body.byModel()
	for _, id := range []string{mappedModel, unmappedModel, unmappedModel2} {
		if _, present := keyed[id]; !present {
			t.Fatalf("%s missing from %v", id, keyed)
		}
	}
}

func TestTheLookupIsNotCategoryScoped(t *testing.T) {
	s := newStack(t)

	_, body := s.presentationAssets(mappedModel, otherCategory)

	if len(body.Assets) != 2 {
		t.Fatalf("%d entries, want both categories' Models resolvable: %+v", len(body.Assets), body.Assets)
	}
}

func TestTheLookupRefusesAnEmptyOrOversizedRequest(t *testing.T) {
	s := newStack(t)

	resp, _ := s.do(http.MethodGet, "/catalog/collection-presentation-assets", nil, s.token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no model_ids → %d, want 400", resp.StatusCode)
	}
	resp, _ = s.do(http.MethodGet, "/catalog/collection-presentation-assets?model_ids=,,", nil, s.token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank model_ids → %d, want 400", resp.StatusCode)
	}

	tooMany := make([]string, catalogapp.MaxPresentationAssetLookup+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("dev-fixture:model-%d", index)
	}
	resp, _ = s.presentationAssets(tooMany...)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("%d ids → %d, want 400", len(tooMany), resp.StatusCode)
	}

	atBound := tooMany[:catalogapp.MaxPresentationAssetLookup]
	atBound[0] = mappedModel
	resp, body := s.presentationAssets(atBound...)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d ids → %d, want 200", len(atBound), resp.StatusCode)
	}
	if len(body.Assets) != 1 {
		t.Fatalf("%d entries, want 1 (only one id names a real Model)", len(body.Assets))
	}
}

// MARK: - The mapping's target is publishable, and delivery is unchanged

func TestAMappedModelsBundleIsPublishableAndDeliverable(t *testing.T) {
	s := newStack(t)

	published := s.publishCommittedModelFixture(t)
	if published.AlreadyPublished {
		t.Fatal("a fresh stack should publish, not no-op")
	}
	if published.Bundle.Kind != catalogdomain.BundleKindCollectionItem {
		t.Fatalf("published kind %q, want collection_item", published.Bundle.Kind)
	}

	_, body := s.presentationAssets(mappedModel)
	asset := body.byModel()[mappedModel]
	if asset.AssetBundleID != published.Bundle.BundleID {
		t.Fatalf("the Model maps to %q but %q was published", asset.AssetBundleID, published.Bundle.BundleID)
	}

	resp, manifest := s.fetchManifest(t, asset.AssetBundleID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest for the mapped bundle: %d", resp.StatusCode)
	}
	if manifest.Kind != "collection_item" || manifest.Version != asset.AssetBundleVersion {
		t.Fatalf("manifest kind %q v%d, want collection_item v%d",
			manifest.Kind, manifest.Version, asset.AssetBundleVersion)
	}
	var geometry int
	for _, file := range manifest.Files {
		if file.Role == "geometry" {
			geometry++
		}
	}
	if geometry != 1 {
		t.Fatalf("%d geometry files in a Model bundle, want 1", geometry)
	}
	if len(published.Bundle.TierCapacities) != 0 {
		t.Fatalf("a collection_item bundle derived tier capacities: %v", published.Bundle.TierCapacities)
	}
}

func TestSwappingAPlaceholderForAuthoredArtIsADataChangeOnly(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.publishCommittedModelFixture(t)

	roomID := s.roomWithPublishedDesign(t, "Watches")
	room := s.mustAddItem(roomID, mappedModel)
	itemID := room.Items[0].ID
	contentBefore := s.roomState(t, roomID)

	dir := committedModelFixtureDir(t)
	geometry, err := os.ReadFile(filepath.Join(dir, "geometry.usda"))
	if err != nil {
		t.Fatal(err)
	}
	authored := string(geometry) + "\n# a later, differently-authored export\n"
	if _, err := s.publisher.Publish(ctx, catalogapp.PublishRequest{
		BundleID: collectionModel, Version: 2, Kind: catalogdomain.BundleKindCollectionItem,
		Format: "usda", MinAppVersion: 1,
		Files: []catalogapp.PublishSource{
			newPublishSource("geometry", catalogdomain.RoleGeometry, "model/vnd.usda+ascii", authored),
		},
	}); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	if _, manifest := s.fetchManifest(t, collectionModel, ""); manifest.Version != 2 {
		t.Fatalf("the manifest still serves v%d after publishing v2", manifest.Version)
	}

	t.Cleanup(func() {
		_, _ = s.pool.Pool().Exec(context.Background(),
			`UPDATE collection_models SET asset_bundle_id = $2, asset_bundle_version = 1 WHERE id = $1`,
			mappedModel, collectionModel)
	})
	if _, err := s.pool.Pool().Exec(ctx,
		`UPDATE collection_models SET asset_bundle_id = 'bundle_authored_watch', asset_bundle_version = 3
		 WHERE id = $1`, mappedModel); err != nil {
		t.Fatal(err)
	}
	_, body := s.presentationAssets(mappedModel)
	asset := body.byModel()[mappedModel]
	if asset.AssetBundleID != "bundle_authored_watch" || asset.AssetBundleVersion != 3 {
		t.Fatalf("after re-pointing, the mapping serves %s v%d", asset.AssetBundleID, asset.AssetBundleVersion)
	}

	if after := s.roomState(t, roomID); after != contentBefore {
		t.Fatalf("an asset swap changed Collection Item content:\nbefore %s\nafter  %s", contentBefore, after)
	}
	bySlot, _ := s.readItems(roomID)
	if bySlot[0] != itemID {
		t.Fatalf("slot 0 holds %q, want %q", bySlot[0], itemID)
	}
	page := s.mustSearch("category_id=" + seededCategory + "&q=chrono+one")
	if len(page.Models) != 1 || page.Models[0].ID != mappedModel {
		t.Fatalf("Manual Search changed across the swap: %+v", page.Models)
	}
	if !page.Models[0].HasAsset {
		t.Fatal("Manual Search stopped reporting the Model's asset after the swap")
	}
}

// MARK: - Content stays free of presentation

func TestCollectionItemContentStoresOnlyModelIdentity(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	rows, err := s.pool.Pool().Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'collection_items' ORDER BY column_name
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"catalog_model_id", "collection_room_id", "created_at", "id", "slot_index", "updated_at",
	}
	if strings.Join(columns, ",") != strings.Join(expected, ",") {
		t.Fatalf("collection_items columns are %v, want exactly %v — presentation must not enter content",
			columns, expected)
	}

	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.mustAddItem(roomID, mappedModel)
	resp, raw := s.do(http.MethodGet, "/collection-rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read room: %d", resp.StatusCode)
	}
	for _, forbidden := range []string{
		"asset_bundle_id", "asset_bundle_version", "geometry", "material", "transform",
		"scale", "rotation", "position", "mesh", "lighting", "shadow", "mount",
	} {
		if strings.Contains(string(raw), `"`+forbidden+`"`) {
			t.Fatalf("the Collection Room payload names %q — presentation must not leak into content", forbidden)
		}
	}
}

func TestOnlyPresentationTablesCarryAnAssetReference(t *testing.T) {
	s := newStack(t)

	rows, err := s.pool.Pool().Query(context.Background(), `
		SELECT table_name FROM information_schema.columns
		WHERE column_name IN ('asset_bundle_id', 'asset_bundle_version')
		GROUP BY table_name ORDER BY table_name
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	expected := []string{"collection_designs", "collection_models", "museum_styles", "room_variants", "sculptures"}
	if strings.Join(tables, ",") != strings.Join(expected, ",") {
		t.Fatalf("asset references live on %v, want exactly %v", tables, expected)
	}
	if len(tables) == 0 {
		t.Fatal("the scan found nothing — the test is broken, not the schema")
	}
}

// MARK: - Production gating

func TestProductionRefusesFixtureMappings(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	repo := catalinfra.NewPostgresCatalogRepository(s.pool.Pool())
	development := catalogapp.NewCollectionCatalogService(repo, false)
	production := catalogapp.NewCollectionCatalogService(repo, true)

	inDevelopment, err := development.PresentationAssetMappings(ctx, []string{mappedModel, unmappedModel})
	if err != nil {
		t.Fatal(err)
	}
	if len(inDevelopment) != 2 {
		t.Fatalf("development served %d mappings, want 2", len(inDevelopment))
	}

	inProduction, err := production.PresentationAssetMappings(ctx, []string{mappedModel, unmappedModel})
	if err != nil {
		t.Fatal(err)
	}
	if len(inProduction) != 0 {
		t.Fatalf("production served %d fixture mappings, want 0: %+v", len(inProduction), inProduction)
	}
}
