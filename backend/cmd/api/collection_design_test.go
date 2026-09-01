package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	catalogapp "muse-backend/internal/catalog/application"
	catalogdomain "muse-backend/internal/catalog/domain"
	catalinfra "muse-backend/internal/catalog/infrastructure"
)

type collectionDesignJSON struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"display_name"`
	CategoryID           string `json:"category_id"`
	IsDevelopmentFixture bool   `json:"is_development_fixture"`
	AssetBundleID        string `json:"asset_bundle_id"`
	AssetBundleVersion   int    `json:"asset_bundle_version"`
	SortOrder            int    `json:"sort_order"`
}

type collectionDesignListJSON struct {
	CollectionDesigns []collectionDesignJSON `json:"collection_designs"`
}

const devFixtureDesign = catalogdomain.DesignDevFixtureID

func (s *stack) fetchDesigns(token, categoryID string) collectionDesignListJSON {
	s.t.Helper()
	resp, body := s.do(http.MethodGet, "/catalog/collection-designs?category_id="+categoryID, nil, token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("list designs for %s: %d %s", categoryID, resp.StatusCode, body)
	}
	var list collectionDesignListJSON
	if err := json.Unmarshal(body, &list); err != nil {
		s.t.Fatalf("decode designs: %v (%s)", err, body)
	}
	return list
}

func (s *stack) insertDesign(id, categoryID, classification string) {
	s.t.Helper()
	ctx := context.Background()
	_, err := s.pool.Pool().Exec(ctx, `
		INSERT INTO collection_designs (id, category_id, display_name, classification, asset_bundle_id, asset_bundle_version, sort_order)
		VALUES ($1, NULLIF($2, ''), $3, $4, 'bundle_test', 1, 10)
		ON CONFLICT (id) DO NOTHING
	`, id, categoryID, "Test "+id, classification)
	if err != nil {
		s.t.Fatalf("insert design %s: %v", id, err)
	}
	s.t.Cleanup(func() {
		if _, err := s.pool.Pool().Exec(context.Background(),
			`UPDATE collection_rooms SET design_id = NULL WHERE design_id = $1`, id); err != nil {
			s.t.Errorf("cleanup rooms for %s: %v", id, err)
		}
		if _, err := s.pool.Pool().Exec(context.Background(),
			`DELETE FROM collection_designs WHERE id = $1`, id); err != nil {
			s.t.Errorf("cleanup design %s: %v", id, err)
		}
	})
}

func TestUniversalDesignAppearsForEveryCategory(t *testing.T) {
	s := newStack(t)

	for _, category := range s.fetchCategories(s.token).CollectionCategories {
		designs := s.fetchDesigns(s.token, category.ID)

		var found *collectionDesignJSON
		for index := range designs.CollectionDesigns {
			if designs.CollectionDesigns[index].ID == devFixtureDesign {
				found = &designs.CollectionDesigns[index]
			}
		}
		if found == nil {
			t.Fatalf("the universal Design is missing for category %q", category.ID)
		}
		if found.CategoryID != "" {
			t.Fatalf("the universal Design carries category %q; universal must serialise as an absent scope", found.CategoryID)
		}
		if !found.IsDevelopmentFixture {
			t.Fatal("the fixture Design is not flagged as a development fixture")
		}
		if !strings.Contains(found.DisplayName, "Development") {
			t.Fatalf("display name %q does not identify itself as a placeholder", found.DisplayName)
		}
		if found.AssetBundleID == "" || found.AssetBundleVersion <= 0 {
			t.Fatalf("the Design carries no usable bundle identity: %+v", found)
		}
	}
}

func TestScopedDesignAppearsAndPersistsOnlyForItsCategory(t *testing.T) {
	s := newStack(t)
	s.insertDesign("design_test_watches", "category_watches", "production")

	watchesRoom := s.createCollectionRoomInCategory(s.token, "Watches", "category_watches")
	coinsRoom := s.createCollectionRoomInCategory(s.token, "Coins", "category_coins")

	watchesDesigns := s.fetchDesigns(s.token, "category_watches")
	if !containsDesign(watchesDesigns, "design_test_watches") {
		t.Fatal("the Watches-scoped Design is missing from the Watches list")
	}
	for _, other := range []string{"category_coins", "category_hot_wheels", "category_license_plates"} {
		if containsDesign(s.fetchDesigns(s.token, other), "design_test_watches") {
			t.Fatalf("the Watches-scoped Design leaked into %q's list", other)
		}
	}

	resp, body := s.do(http.MethodPatch, "/collection-rooms/"+watchesRoom,
		map[string]string{"design_id": "design_test_watches"}, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign a matching Design: %d %s", resp.StatusCode, body)
	}
	if got := decodeCollectionRoom(t, body); got.DesignID != "design_test_watches" {
		t.Fatalf("design_id = %q after assignment", got.DesignID)
	}

	resp, body = s.do(http.MethodPatch, "/collection-rooms/"+coinsRoom,
		map[string]string{"design_id": "design_test_watches"}, s.token)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "design_not_applicable") {
		t.Fatalf("assigning a foreign-category Design → %d %s; want 400 design_not_applicable", resp.StatusCode, body)
	}
	resp, body = s.do(http.MethodGet, "/collection-rooms/"+coinsRoom, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(body)
	}
	if got := decodeCollectionRoom(t, body); got.DesignID != "" {
		t.Fatalf("the refused assignment still wrote design %q", got.DesignID)
	}

	resp, body = s.do(http.MethodPatch, "/collection-rooms/"+coinsRoom,
		map[string]string{"design_id": "design_does_not_exist"}, s.token)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "design_not_applicable") {
		t.Fatalf("assigning a nonexistent Design → %d %s; want the same 400 design_not_applicable", resp.StatusCode, body)
	}
}

func containsDesign(list collectionDesignListJSON, id string) bool {
	for _, design := range list.CollectionDesigns {
		if design.ID == id {
			return true
		}
	}
	return false
}

func TestSelectedDesignPersistsAndReloads(t *testing.T) {
	s := newStack(t)
	roomID := s.createCollectionRoom(s.token, "Watches")

	resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"design_id": devFixtureDesign}, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("select design: %d %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodGet, "/collection-rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reload: %d %s", resp.StatusCode, body)
	}
	reloaded := decodeCollectionRoom(t, body)
	if reloaded.DesignID != devFixtureDesign {
		t.Fatalf("design_id = %q after reload, want %q", reloaded.DesignID, devFixtureDesign)
	}
	if reloaded.Name != "Watches" || reloaded.CategoryID != seededCategory || reloaded.CurrentTier != 1 {
		t.Fatalf("a design-only patch disturbed the Room: %+v", reloaded)
	}

	resp, body = s.do(http.MethodGet, "/collection-rooms", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(body)
	}
	var list collectionRoomListJSON
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.CollectionRooms) != 1 || list.CollectionRooms[0].DesignID != devFixtureDesign {
		t.Fatalf("the list does not carry the selected design: %+v", list.CollectionRooms)
	}

	resp, body = s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"design_id": ""}, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear design: %d %s", resp.StatusCode, body)
	}
	if got := decodeCollectionRoom(t, body); got.DesignID != "" {
		t.Fatalf("design_id = %q after clearing", got.DesignID)
	}
}

func TestBundleVersionChangeTouchesNoCollectionRoom(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	roomID := s.createCollectionRoom(s.token, "Watches")

	if resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"design_id": devFixtureDesign}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("select design: %d %s", resp.StatusCode, body)
	}

	roomRow := func() string {
		var row string
		err := s.pool.Pool().QueryRow(ctx, `
			SELECT id::text || '|' || name || '|' || coalesce(category_id,'-') || '|' ||
			       coalesce(design_id,'-') || '|' || current_tier || '|' || updated_at::text
			FROM collection_rooms WHERE id = $1
		`, roomID).Scan(&row)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	before := roomRow()

	if _, err := s.pool.Pool().Exec(ctx,
		`UPDATE collection_designs SET asset_bundle_id = 'bundle_authored_real', asset_bundle_version = 7 WHERE id = $1`,
		devFixtureDesign); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Pool().Exec(context.Background(),
			`UPDATE collection_designs SET asset_bundle_id = 'dev_fixture_collection_design', asset_bundle_version = 1 WHERE id = $1`,
			devFixtureDesign)
	})

	if after := roomRow(); after != before {
		t.Fatalf("a bundle re-point changed the Collection Room row:\nbefore %s\nafter  %s", before, after)
	}

	designs := s.fetchDesigns(s.token, seededCategory)
	for _, design := range designs.CollectionDesigns {
		if design.ID != devFixtureDesign {
			continue
		}
		if design.AssetBundleID != "bundle_authored_real" || design.AssetBundleVersion != 7 {
			t.Fatalf("the Design still serves %s v%d after the re-point", design.AssetBundleID, design.AssetBundleVersion)
		}
		return
	}
	t.Fatal("the Design vanished from the list after a bundle re-point")
}

func TestACategoryChangeThatWouldStrandTheDesignIsRefusedWhole(t *testing.T) {
	s := newStack(t)
	s.insertDesign("design_test_watches", "category_watches", "production")
	roomID := s.createCollectionRoomInCategory(s.token, "Watches", "category_watches")

	if resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"design_id": "design_test_watches"}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("seed the design: %d %s", resp.StatusCode, body)
	}

	resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"category_id": "category_coins"}, s.token)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "design_not_applicable") {
		t.Fatalf("stranding category change → %d %s; want 400 design_not_applicable", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodGet, "/collection-rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(body)
	}
	after := decodeCollectionRoom(t, body)
	if after.CategoryID != "category_watches" || after.DesignID != "design_test_watches" {
		t.Fatalf("the refused patch changed the Room: %+v", after)
	}

	s.insertDesign("design_test_coins", "category_coins", "production")
	resp, body = s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"category_id": "category_coins", "design_id": "design_test_coins"}, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("changing category and design together: %d %s", resp.StatusCode, body)
	}
	moved := decodeCollectionRoom(t, body)
	if moved.CategoryID != "category_coins" || moved.DesignID != "design_test_coins" {
		t.Fatalf("the combined patch did not apply: %+v", moved)
	}

	if resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"design_id": devFixtureDesign}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("assign the universal design: %d %s", resp.StatusCode, body)
	}
	if resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"category_id": "category_hot_wheels"}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("re-categorising with a universal design: %d %s", resp.StatusCode, body)
	}
}

func TestDesignListingRequiresARealCategory(t *testing.T) {
	s := newStack(t)

	for _, query := range []string{"", "?category_id=", "?category_id=category_stamps", "?category_id=Watches"} {
		resp, body := s.do(http.MethodGet, "/catalog/collection-designs"+query, nil, s.token)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET /catalog/collection-designs%s → %d %s; want 400", query, resp.StatusCode, body)
		}
	}

	if resp, _ := s.do(http.MethodGet, "/catalog/collection-designs?category_id="+seededCategory, nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated → %d, want 401", resp.StatusCode)
	}
	mine := s.fetchDesigns(s.token, seededCategory)
	theirs := s.fetchDesigns(s.strangerToken(), seededCategory)
	if len(mine.CollectionDesigns) != len(theirs.CollectionDesigns) {
		t.Fatal("the Design catalog differs between callers — it is Platform data and must not")
	}
}

func TestDevFixtureIsRefusedInProduction(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	catalog := catalinfra.NewPostgresCatalogRepository(s.pool.Pool())

	development := catalogapp.NewCollectionDesignService(catalog, false)
	production := catalogapp.NewCollectionDesignService(catalog, true)

	devList, err := development.ApplicableDesigns(ctx, seededCategory)
	if err != nil {
		t.Fatal(err)
	}
	if len(devList) == 0 {
		t.Fatal("development should serve the fixture")
	}

	prodList, err := production.ApplicableDesigns(ctx, seededCategory)
	if err != nil {
		t.Fatal(err)
	}
	for _, design := range prodList {
		if design.IsDevelopmentFixture() {
			t.Fatalf("a production deployment served development fixture %q", design.ID)
		}
	}

	applicable, err := production.IsDesignApplicable(ctx, devFixtureDesign, seededCategory)
	if err != nil {
		t.Fatal(err)
	}
	if applicable {
		t.Fatal("a production deployment accepted a development fixture Design")
	}
	applicable, err = development.IsDesignApplicable(ctx, devFixtureDesign, seededCategory)
	if err != nil {
		t.Fatal(err)
	}
	if !applicable {
		t.Fatal("development refused the fixture — then the production result proves nothing")
	}
}

func TestCollectionDesignAndMuseumStyleAreUnrelated(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	forbidden := map[string][]string{
		"collection_designs": {"style_id", "variant_id", "museum_id", "room_id"},
		"museum_styles":      {"collection_design_id", "collection_category_id"},
		"room_variants":      {"collection_design_id", "collection_category_id"},
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
				t.Errorf("%s.%s exists — `01` §5.1 makes Collection design choices distinct from Museum styles", table, column)
			}
		}
	}

	museumPresentation := map[string]bool{"museum_styles": true, "room_variants": true}
	collectionPresentation := map[string]bool{"collection_designs": true, "collection_categories": true}
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
		if museumPresentation[from] && collectionPresentation[to] {
			t.Errorf("foreign key %s → %s crosses the Museum/Collection presentation boundary", from, to)
		}
		if collectionPresentation[from] && museumPresentation[to] {
			t.Errorf("foreign key %s → %s crosses the Museum/Collection presentation boundary", from, to)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var referenced string
	if err := s.pool.Pool().QueryRow(ctx, `
		SELECT coalesce(string_agg(DISTINCT confrelid::regclass::text, ','), '')
		FROM pg_constraint
		WHERE contype = 'f' AND conrelid = 'collection_designs'::regclass
	`).Scan(&referenced); err != nil {
		t.Fatal(err)
	}
	if referenced != "collection_categories" {
		t.Fatalf("collection_designs references %q; gives it exactly one relationship, to collection_categories", referenced)
	}
}

func TestADesignInUseCannotBeDeleted(t *testing.T) {
	s := newStack(t)
	roomID := s.createCollectionRoom(s.token, "Watches")
	if resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"design_id": devFixtureDesign}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("select design: %d %s", resp.StatusCode, body)
	}

	if _, err := s.pool.Pool().Exec(context.Background(),
		`DELETE FROM collection_designs WHERE id = $1`, devFixtureDesign); err == nil {
		t.Fatal("a referenced Design was deleted — ON DELETE RESTRICT is missing")
	}
}
