package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	catalogapp "muse-backend/internal/catalog/application"
	catalinfra "muse-backend/internal/catalog/infrastructure"
)

type modelJSON struct {
	ID                   string          `json:"id"`
	BrandID              string          `json:"brand_id"`
	BrandDisplayName     string          `json:"brand_display_name"`
	CategoryID           string          `json:"category_id"`
	DisplayName          string          `json:"display_name"`
	Metadata             json.RawMessage `json:"metadata"`
	HasAsset             bool            `json:"has_asset"`
	AssetBundleID        string          `json:"asset_bundle_id"`
	AssetBundleVersion   int             `json:"asset_bundle_version"`
	IsDevelopmentFixture bool            `json:"is_development_fixture"`
}

type modelSearchJSON struct {
	Models         []modelJSON `json:"models"`
	NextCursorName string      `json:"next_cursor_name"`
	NextCursorID   string      `json:"next_cursor_id"`
}

func (s *stack) searchModels(query string) (*http.Response, modelSearchJSON) {
	s.t.Helper()
	resp, body := s.do(http.MethodGet, "/catalog/collection-models?"+query, nil, s.token)
	var page modelSearchJSON
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &page); err != nil {
			s.t.Fatalf("decode search page: %v (%s)", err, body)
		}
	}
	return resp, page
}

func (s *stack) mustSearch(query string) modelSearchJSON {
	s.t.Helper()
	resp, page := s.searchModels(query)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("search %q: %d", query, resp.StatusCode)
	}
	return page
}

func ids(page modelSearchJSON) []string {
	out := make([]string, 0, len(page.Models))
	for _, model := range page.Models {
		out = append(out, model.ID)
	}
	return out
}

func TestManualSearchWorksWithNoMLPresent(t *testing.T) {
	s := newStack(t)

	page := s.mustSearch("category_id=category_watches&q=devco")

	if len(page.Models) == 0 {
		t.Fatal("Manual Search returned nothing with no ML system present")
	}
	resp, body := s.do(http.MethodGet,
		"/catalog/collection-models?category_id=category_watches&q=devco", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(body)
	}
	for _, forbidden := range []string{"confidence", "candidate", "recognition", "score"} {
		if strings.Contains(strings.ToLower(string(body)), forbidden) {
			t.Fatalf("the search payload carries %q — Manual Search must be independent of recognition", forbidden)
		}
	}
}

func TestCategoryFilteringIsCorrect(t *testing.T) {
	s := newStack(t)

	watches := s.mustSearch("category_id=category_watches")
	cars := s.mustSearch("category_id=category_hot_wheels")

	if len(watches.Models) != 3 {
		t.Fatalf("Watches holds %d fixture models, want 3: %v", len(watches.Models), ids(watches))
	}
	if len(cars.Models) != 1 {
		t.Fatalf("Hot Wheels holds %d fixture models, want 1: %v", len(cars.Models), ids(cars))
	}
	for _, model := range watches.Models {
		if model.CategoryID != "category_watches" {
			t.Fatalf("%s leaked into the Watches results with category %s", model.ID, model.CategoryID)
		}
	}
	devcoWatches := s.mustSearch("category_id=category_watches&q=devco")
	for _, id := range ids(devcoWatches) {
		if id == "dev-fixture:model-racer" {
			t.Fatal("a Hot Wheels model surfaced in a Watches search")
		}
	}
	devcoCars := s.mustSearch("category_id=category_hot_wheels&q=devco")
	if len(devcoCars.Models) != 1 || devcoCars.Models[0].ID != "dev-fixture:model-racer" {
		t.Fatalf("the same term in Hot Wheels returned %v", ids(devcoCars))
	}

	empty := s.mustSearch("category_id=category_coins&q=devco")
	if len(empty.Models) != 0 {
		t.Fatalf("Coins returned %v", ids(empty))
	}
}

func TestBrandAndModelSearchIsRelevantAndDeterministic(t *testing.T) {
	s := newStack(t)

	devco := s.mustSearch("category_id=category_watches&q=devco")
	if len(devco.Models) != 2 {
		t.Fatalf("brand term matched %v, want the two Devco watches", ids(devco))
	}
	for _, model := range devco.Models {
		if model.BrandID != "dev-fixture:brand-a" {
			t.Fatalf("%s is not a Devco model", model.ID)
		}
	}

	one := s.mustSearch("category_id=category_watches&q=devco+chrono+one")
	if len(one.Models) != 1 || one.Models[0].ID != "dev-fixture:model-chrono-one" {
		t.Fatalf("three AND-ed terms matched %v, want exactly Chrono One", ids(one))
	}

	for _, partial := range []string{"dev", "devc", "chron", "diver"} {
		page := s.mustSearch("category_id=category_watches&q=" + partial)
		if len(page.Models) == 0 {
			t.Fatalf("the partial term %q matched nothing — prefix search is not working", partial)
		}
	}

	if page := s.mustSearch("category_id=category_watches&q=zzzznotathing"); len(page.Models) != 0 {
		t.Fatalf("a nonsense term matched %v", ids(page))
	}

	first := ids(s.mustSearch("category_id=category_watches"))
	for attempt := 0; attempt < 5; attempt++ {
		again := ids(s.mustSearch("category_id=category_watches"))
		if strings.Join(again, ",") != strings.Join(first, ",") {
			t.Fatalf("run %d returned %v, first run returned %v", attempt, again, first)
		}
	}
	names := make([]string, 0, len(first))
	for _, model := range s.mustSearch("category_id=category_watches").Models {
		names = append(names, model.DisplayName)
	}
	for index := 1; index < len(names); index++ {
		if names[index-1] > names[index] {
			t.Fatalf("results are not ordered by display_name: %v", names)
		}
	}
}

func TestMatchingIgnoresCaseAndPunctuation(t *testing.T) {
	s := newStack(t)

	baseline := ids(s.mustSearch("category_id=category_watches&q=devco+chrono"))
	if len(baseline) != 2 {
		t.Fatalf("baseline matched %v", baseline)
	}
	for _, variant := range []string{"DEVCO+CHRONO", "Devco%2C+Chrono", "devco-chrono", "++devco+++chrono++"} {
		got := ids(s.mustSearch("category_id=category_watches&q=" + variant))
		if strings.Join(got, ",") != strings.Join(baseline, ",") {
			t.Fatalf("%q matched %v, baseline %v", variant, got, baseline)
		}
	}

	for _, hostile := range []string{"devco+%26+chrono", "devco%3A%2A", "%21devco", "devco%27%29%3B--"} {
		resp, _ := s.searchModels("category_id=category_watches&q=" + hostile)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("hostile query %q → %d; sanitisation should make it harmless, not an error",
				hostile, resp.StatusCode)
		}
	}
}

func TestPaginationIsKeysetAndCoversEveryRowOnce(t *testing.T) {
	s := newStack(t)

	all := ids(s.mustSearch("category_id=category_watches"))
	if len(all) != 3 {
		t.Fatalf("expected 3 Watches fixtures, got %v", all)
	}

	var walked []string
	query := "category_id=category_watches&limit=1"
	for page := 0; page < 10; page++ {
		result := s.mustSearch(query)
		walked = append(walked, ids(result)...)
		if result.NextCursorID == "" {
			break
		}
		query = fmt.Sprintf("category_id=category_watches&limit=1&cursor_name=%s&cursor_id=%s",
			strings.ReplaceAll(result.NextCursorName, " ", "+"), result.NextCursorID)
	}
	if strings.Join(walked, ",") != strings.Join(all, ",") {
		t.Fatalf("paging one at a time walked %v, want %v", walked, all)
	}

	if resp, _ := s.searchModels("category_id=category_watches&cursor_name=x"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a half cursor → %d, want 400", resp.StatusCode)
	}
	if resp, _ := s.searchModels("category_id=category_watches&cursor_id=x"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a half cursor → %d, want 400", resp.StatusCode)
	}
}

func TestSearchRequiresARealCategory(t *testing.T) {
	s := newStack(t)

	for _, query := range []string{"", "q=devco", "category_id=", "category_id=category_stamps"} {
		resp, _ := s.searchModels(query)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("search %q → %d, want 400", query, resp.StatusCode)
		}
	}

	if resp, _ := s.do(http.MethodGet,
		"/catalog/collection-models?category_id=category_watches", nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated → %d, want 401", resp.StatusCode)
	}
	mine := ids(s.mustSearch("category_id=category_watches"))
	resp, body := s.do(http.MethodGet, "/catalog/collection-models?category_id=category_watches", nil, s.strangerToken())
	if resp.StatusCode != http.StatusOK {
		t.Fatal(body)
	}
	var theirs modelSearchJSON
	if err := json.Unmarshal(body, &theirs); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids(theirs), ",") != strings.Join(mine, ",") {
		t.Fatal("the catalog differs between callers — it is Platform data and must not")
	}
}

func TestModelsWithAndWithoutAssetsAreBothSelectable(t *testing.T) {
	s := newStack(t)

	page := s.mustSearch("category_id=category_watches")

	var withAsset, withoutAsset int
	for _, model := range page.Models {
		if model.HasAsset {
			withAsset++
			if model.AssetBundleID == "" {
				t.Fatalf("%s claims an asset but carries no bundle id", model.ID)
			}
		} else {
			withoutAsset++
			if model.AssetBundleID != "" {
				t.Fatalf("%s has no asset but carries bundle id %q", model.ID, model.AssetBundleID)
			}
		}
	}
	if withAsset == 0 || withoutAsset == 0 {
		t.Fatalf("the fixture must include a Model with an asset and one without; got %d/%d",
			withAsset, withoutAsset)
	}
}

func TestResultsCarryOnlyConfirmedCatalogMetadata(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodGet,
		"/catalog/collection-models?category_id=category_watches&q=devco+chrono+one", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(body)
	}
	var raw struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Models) != 1 {
		t.Fatalf("expected one result, got %d", len(raw.Models))
	}

	allowed := map[string]bool{
		"id": true, "brand_id": true, "brand_display_name": true, "category_id": true,
		"display_name": true, "metadata": true, "has_asset": true,
		"asset_bundle_id": true, "asset_bundle_version": true, "is_development_fixture": true,
	}
	for field := range raw.Models[0] {
		if !allowed[field] {
			t.Errorf("the result carries unexpected field %q", field)
		}
	}
	if raw.Models[0]["brand_display_name"] == "" {
		t.Fatal("the brand's display name is missing from the result")
	}
	if raw.Models[0]["is_development_fixture"] != true {
		t.Fatal("a fixture Model must be flagged as one")
	}
}

func TestUnknownModelIdsCannotBecomeCatalogReferences(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	roomID := s.createCollectionRoom(s.token, "Watches")

	_, err := s.pool.Pool().Exec(ctx, `
		INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id)
		VALUES ($1, 0, 'dev-fixture:model-does-not-exist')
	`, roomID)
	if err == nil {
		t.Fatal("an item referencing a nonexistent Model was accepted — collection_items_model_fk is missing")
	}

	if _, err := s.pool.Pool().Exec(ctx, `
		INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id)
		VALUES ($1, 0, 'dev-fixture:model-chrono-one')
	`, roomID); err != nil {
		t.Fatalf("a real Model reference was refused: %v", err)
	}

	if _, err := s.pool.Pool().Exec(ctx,
		`DELETE FROM collection_models WHERE id = 'dev-fixture:model-chrono-one'`); err == nil {
		t.Fatal("a referenced Model was deleted — ON DELETE RESTRICT is missing")
	}
}

func TestCatalogIsPlatformOwnedAndContentStaysIndependent(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	forbidden := map[string][]string{
		"collection_brands": {"account_id", "collection_room_id", "museum_id", "room_id", "owner_id"},
		"collection_models": {"account_id", "collection_room_id", "museum_id", "room_id", "owner_id"},
	}
	for table, columns := range forbidden {
		for _, column := range columns {
			var exists bool
			if err := s.pool.Pool().QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM information_schema.columns
				               WHERE table_name = $1 AND column_name = $2)
			`, table, column).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Errorf("%s.%s exists — the catalog is \"not owned by any individual User, Museum, or Collection Room\" (`04`)", table, column)
			}
		}
	}

	catalogTables := map[string]bool{
		"collection_brands": true, "collection_models": true,
		"collection_categories": true, "collection_designs": true,
	}
	contentTables := map[string]bool{
		"collection_rooms": true, "collection_items": true,
		"museums": true, "rooms": true, "room_photo_slots": true, "room_sculptures": true,
		"accounts": true,
	}
	rows, err := s.pool.Pool().Query(ctx,
		`SELECT conrelid::regclass::text, confrelid::regclass::text FROM pg_constraint WHERE contype = 'f'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			t.Fatal(err)
		}
		if catalogTables[from] && contentTables[to] {
			t.Errorf("foreign key %s → %s: the catalog must never reference content", from, to)
		}
		if (from == "museums" || from == "rooms" || from == "room_photo_slots" || from == "room_sculptures") &&
			catalogTables[to] && to != "collection_categories" {
			t.Errorf("foreign key %s → %s: the Museum tree must stay independent of Collection catalog entities", from, to)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	before := s.snapshotOwnerState()
	s.mustSearch("category_id=category_watches&q=devco")
	if after := s.snapshotOwnerState(); after != before {
		t.Fatal("a catalog search changed the owner's content")
	}
}

func TestAddingCatalogDataNeedsNoRelease(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	if _, err := s.pool.Pool().Exec(ctx, `
		INSERT INTO collection_brands (id, display_name, sort_order, classification)
		VALUES ('dev-fixture:brand-late', 'Latecomer (development fixture)', 9020, 'dev_fixture')
		ON CONFLICT (id) DO NOTHING
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Pool().Exec(ctx, `
		INSERT INTO collection_models
			(id, brand_id, category_id, display_name, search_text, classification)
		VALUES ('dev-fixture:model-late', 'dev-fixture:brand-late', 'category_watches',
		        'Latecomer Alpha (development fixture)', 'latecomer alpha development fixture watch', 'dev_fixture')
		ON CONFLICT (id) DO NOTHING
	`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		background := context.Background()
		if _, err := s.pool.Pool().Exec(background,
			`DELETE FROM collection_items WHERE catalog_model_id = 'dev-fixture:model-late'`); err != nil {
			t.Errorf("cleanup items: %v", err)
		}
		if _, err := s.pool.Pool().Exec(background,
			`DELETE FROM collection_models WHERE id = 'dev-fixture:model-late'`); err != nil {
			t.Errorf("cleanup model: %v", err)
		}
		if _, err := s.pool.Pool().Exec(background,
			`DELETE FROM collection_brands WHERE id = 'dev-fixture:brand-late'`); err != nil {
			t.Errorf("cleanup brand: %v", err)
		}
	})

	page := s.mustSearch("category_id=category_watches&q=latecomer")
	if len(page.Models) != 1 || page.Models[0].ID != "dev-fixture:model-late" {
		t.Fatalf("a directly-inserted Model was not searchable: %v", ids(page))
	}
	if page.Models[0].BrandDisplayName != "Latecomer (development fixture)" {
		t.Fatalf("brand name = %q", page.Models[0].BrandDisplayName)
	}
	if err := catalinfra.NewPostgresCatalogRepository(s.pool.Pool()).EnsureSeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	if page := s.mustSearch("category_id=category_watches&q=latecomer"); len(page.Models) != 1 {
		t.Fatal("re-seeding removed a directly-added Model — the seed must be a starting set, not an authority that prunes")
	}
}

func TestProductionRefusesFixtureCatalogContent(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	catalog := catalinfra.NewPostgresCatalogRepository(s.pool.Pool())

	development, err := catalogapp.NewCollectionCatalogService(catalog, false).
		SearchModels(ctx, "category_watches", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(development.Models) == 0 {
		t.Fatal("development should serve the fixture catalog")
	}

	production, err := catalogapp.NewCollectionCatalogService(catalog, true).
		SearchModels(ctx, "category_watches", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range production.Models {
		if model.IsDevelopmentFixture() {
			t.Fatalf("a production deployment served fixture Model %q", model.ID)
		}
	}
	if len(production.Models) != 0 {
		t.Fatalf("production served %d models; the real catalog is empty", len(production.Models))
	}
}

func TestLookupByModelIdReadsTheSameRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	catalog := catalinfra.NewPostgresCatalogRepository(s.pool.Pool())

	found, ok, err := catalog.FindCollectionModel(ctx, "dev-fixture:model-chrono-one")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("a seeded Model was not found by id")
	}
	if found.BrandDisplayName == "" || found.CategoryID != "category_watches" {
		t.Fatalf("the by-id read returned an incomplete Model: %+v", found)
	}

	page := s.mustSearch("category_id=category_watches&q=devco+chrono+one")
	if len(page.Models) != 1 || page.Models[0].ID != string(found.ID) {
		t.Fatal("search and lookup-by-id disagree about which Model this is")
	}

	_, ok, err = catalog.FindCollectionModel(ctx, "dev-fixture:model-nope")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("an unknown id was found")
	}
}
