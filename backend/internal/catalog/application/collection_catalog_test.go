package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"muse-backend/internal/catalog/application"
	"muse-backend/internal/catalog/domain"
)

// MARK: - Term sanitisation

func TestSearchTerms_DropsEverythingThatIsNotALetterOrDigit(t *testing.T) {
	cases := map[string][]string{
		"":                        nil,
		"   ":                     nil,
		"devco":                   {"devco"},
		"Devco Chrono":            {"devco", "chrono"},
		"  Devco   Chrono  ":      {"devco", "chrono"},
		"Ref. 5711/1A":            {"ref", "5711", "1a"},
		"Devco-Chrono":            {"devco", "chrono"},
		"DEVCO":                   {"devco"},
		"café":                    {"café"},
		"日本":                      {"日本"},
		"devco & chrono":          {"devco", "chrono"},
		"devco | chrono":          {"devco", "chrono"},
		"!devco":                  {"devco"},
		"devco:*":                 {"devco"},
		"(devco)":                 {"devco"},
		"devco'; DROP TABLE x;--": {"devco", "drop", "table", "x"},
	}
	for input, want := range cases {
		got := application.SearchTerms(input)
		if len(got) != len(want) {
			t.Fatalf("SearchTerms(%q) = %v, want %v", input, got, want)
			continue
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("SearchTerms(%q) = %v, want %v", input, got, want)
			}
		}
	}
}

func TestSearchTerms_IsBounded(t *testing.T) {
	long := strings.Repeat("a", 500)
	terms := application.SearchTerms(long)
	if len(terms) != 1 || len(terms[0]) > 64 {
		t.Fatalf("a 500-character word produced %d terms, first of length %d", len(terms), len(terms[0]))
	}

	many := strings.Repeat("word ", 100)
	if got := len(application.SearchTerms(many)); got > 12 {
		t.Fatalf("100 words produced %d terms; the cap is 12", got)
	}
}

// MARK: - The service

type fakeCatalog struct {
	models     []domain.CollectionModel
	categories map[string]bool
	queries    []domain.ModelSearchQuery
	err        error
	next       *domain.ModelSearchCursor
}

func newFakeCatalog(models ...domain.CollectionModel) *fakeCatalog {
	return &fakeCatalog{
		models:     models,
		categories: map[string]bool{"category_watches": true, "category_hot_wheels": true},
	}
}

func (f *fakeCatalog) SearchCollectionModels(
	_ context.Context, query domain.ModelSearchQuery,
) (domain.ModelSearchPage, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return domain.ModelSearchPage{}, f.err
	}
	return domain.ModelSearchPage{Models: f.models, Next: f.next}, nil
}

func (f *fakeCatalog) CollectionCategoryExists(_ context.Context, categoryID string) (bool, error) {
	return f.categories[categoryID], nil
}

func (f *fakeCatalog) FindCollectionModels(
	_ context.Context, modelIDs []string,
) ([]domain.CollectionModel, error) {
	if f.err != nil {
		return nil, f.err
	}
	wanted := map[string]struct{}{}
	for _, id := range modelIDs {
		wanted[id] = struct{}{}
	}
	var found []domain.CollectionModel
	for _, model := range f.models {
		if _, ok := wanted[string(model.ID)]; ok {
			found = append(found, model)
		}
	}
	return found, nil
}

func (f *fakeCatalog) FindCollectionModel(
	_ context.Context, modelID string,
) (domain.CollectionModel, bool, error) {
	if f.err != nil {
		return domain.CollectionModel{}, false, f.err
	}
	for _, model := range f.models {
		if string(model.ID) == modelID {
			return model, true, nil
		}
	}
	return domain.CollectionModel{}, false, nil
}

func model(id string, classification domain.CatalogContentClassification) domain.CollectionModel {
	return domain.CollectionModel{
		ID:             domain.CollectionModelID(id),
		BrandID:        "dev-fixture:brand-a",
		CategoryID:     "category_watches",
		DisplayName:    id,
		Classification: classification,
	}
}

func TestSearchModels_RequiresARealCategory(t *testing.T) {
	catalog := newFakeCatalog()
	service := application.NewCollectionCatalogService(catalog, false)
	ctx := context.Background()

	if _, err := service.SearchModels(ctx, "", "devco", 0, nil); !errors.Is(err, domain.ErrSearchCategoryRequired) {
		t.Fatalf("no category = %v, want ErrSearchCategoryRequired", err)
	}
	if _, err := service.SearchModels(ctx, "category_stamps", "devco", 0, nil); !errors.Is(err, domain.ErrSearchUnknownCategory) {
		t.Fatalf("unknown category = %v, want ErrSearchUnknownCategory", err)
	}
	if len(catalog.queries) != 0 {
		t.Fatal("no query may reach the database for a missing or unknown category")
	}
}

func TestSearchModels_PassesTheScopeAndCleanedTermsDown(t *testing.T) {
	catalog := newFakeCatalog()
	service := application.NewCollectionCatalogService(catalog, false)

	if _, err := service.SearchModels(context.Background(), "category_watches", "Devco Chrono!", 0, nil); err != nil {
		t.Fatal(err)
	}
	if len(catalog.queries) != 1 {
		t.Fatalf("recorded %d queries, want 1", len(catalog.queries))
	}
	query := catalog.queries[0]
	if query.CategoryID != "category_watches" {
		t.Fatalf("CategoryID = %q", query.CategoryID)
	}
	if len(query.Terms) != 2 || query.Terms[0] != "devco" || query.Terms[1] != "chrono" {
		t.Fatalf("Terms = %v, want [devco chrono]", query.Terms)
	}
}

func TestSearchModels_AnEmptyQueryBrowsesTheCategory(t *testing.T) {
	catalog := newFakeCatalog(model("m1", domain.CatalogProduction))
	service := application.NewCollectionCatalogService(catalog, false)

	page, err := service.SearchModels(context.Background(), "category_watches", "   ", 0, nil)
	if err != nil {
		t.Fatalf("an empty query must browse, not fail: %v", err)
	}
	if len(page.Models) != 1 {
		t.Fatalf("browse returned %d models", len(page.Models))
	}
	if len(catalog.queries[0].Terms) != 0 {
		t.Fatalf("Terms = %v, want empty for a browse", catalog.queries[0].Terms)
	}
}

func TestSearchModels_ClampsTheLimit(t *testing.T) {
	catalog := newFakeCatalog()
	service := application.NewCollectionCatalogService(catalog, false)
	ctx := context.Background()

	for supplied, want := range map[int]int{
		0:    application.DefaultModelSearchLimit,
		-5:   application.DefaultModelSearchLimit,
		10:   10,
		1000: application.MaxModelSearchLimit,
	} {
		catalog.queries = nil
		if _, err := service.SearchModels(ctx, "category_watches", "", supplied, nil); err != nil {
			t.Fatal(err)
		}
		if got := catalog.queries[0].Limit; got != want {
			t.Fatalf("limit %d clamped to %d, want %d", supplied, got, want)
		}
	}
}

func TestSearchModels_ProductionRefusesFixtureContent(t *testing.T) {
	models := []domain.CollectionModel{
		model("dev-fixture:model-a", domain.CatalogDevFixture),
		model("model_real", domain.CatalogProduction),
	}
	ctx := context.Background()

	development, err := application.NewCollectionCatalogService(newFakeCatalog(models...), false).
		SearchModels(ctx, "category_watches", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(development.Models) != 2 {
		t.Fatalf("development served %d models, want both", len(development.Models))
	}

	production, err := application.NewCollectionCatalogService(newFakeCatalog(models...), true).
		SearchModels(ctx, "category_watches", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(production.Models) != 1 || production.Models[0].ID != "model_real" {
		t.Fatalf("production served %+v — fixture content must never reach it",
			production.Models)
	}
}

func TestSearchModels_PassesTheCursorThroughAndReportsTheNextOne(t *testing.T) {
	catalog := newFakeCatalog(model("m1", domain.CatalogProduction))
	catalog.next = &domain.ModelSearchCursor{DisplayName: "Devco Chrono One", ID: "m1"}
	service := application.NewCollectionCatalogService(catalog, false)

	cursor := &domain.ModelSearchCursor{DisplayName: "Devco Chrono", ID: "m0"}
	page, err := service.SearchModels(context.Background(), "category_watches", "", 0, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.queries[0].Cursor != cursor {
		t.Fatal("the cursor must reach the query unchanged")
	}
	if page.Next == nil || page.Next.ID != "m1" {
		t.Fatalf("Next = %+v, want the page's own last row", page.Next)
	}
}

func TestCollectionCatalogService_HasNoRecognitionDependency(t *testing.T) {
	service := application.NewCollectionCatalogService(newFakeCatalog(), false)

	page, err := service.SearchModels(context.Background(), "category_watches", "devco", 0, nil)
	if err != nil {
		t.Fatalf("search must work with no ML system present: %v", err)
	}
	_ = page
}

func TestCollectionModel_MayHaveNoAssetYet(t *testing.T) {
	withAsset := domain.CollectionModel{
		AssetBundle: domain.AssetBundleRef{ID: "bundle_x", Version: 2},
	}
	withoutAsset := domain.CollectionModel{}

	if !withAsset.HasAsset() {
		t.Fatal("a Model with a bundle should report an asset")
	}
	if withoutAsset.HasAsset() {
		t.Fatal("a Model with no bundle must not claim an asset")
	}
}

// MARK: -: DesignSlotCapacity resolves the EFFECTIVE version

func TestDesignSlotCapacity_UsesTheVersionTheClientsGenerationResolves(t *testing.T) {
	designs := newFakeDesigns()
	designs.designs["design_fixture"] = domain.CollectionDesign{
		ID: "design_fixture", DisplayName: "Fixture", Classification: domain.DesignDevFixture,
		AssetBundle: domain.AssetBundleRef{ID: "bundle_design", Version: 1}, TierCount: 3,
	}
	bundles := &slotCapacityBundleRepo{versions: map[int]domain.AssetBundle{
		1: {BundleID: "bundle_design", Version: 1, MinAppVersion: 1,
			TierCapacities: domain.TierCapacities{{Tier: 1, Cumulative: 4}, {Tier: 2, Cumulative: 10}, {Tier: 3, Cumulative: 18}}},
		2: {BundleID: "bundle_design", Version: 2, MinAppVersion: 2,
			TierCapacities: domain.TierCapacities{{Tier: 1, Cumulative: 6}, {Tier: 2, Cumulative: 12}, {Tier: 3, Cumulative: 20}}},
	}}
	service := application.NewCollectionDesignService(designs, false).WithBundleRegistry(bundles)
	ctx := context.Background()

	capacity, found, err := service.DesignSlotCapacity(ctx, "design_fixture", 1, 1)
	if err != nil || !found || capacity != 4 {
		t.Fatalf("generation 1, tier 1 = %d, %v, %v; want 4", capacity, found, err)
	}
	capacity, found, err = service.DesignSlotCapacity(ctx, "design_fixture", 2, 1)
	if err != nil || !found || capacity != 6 {
		t.Fatalf("generation 2, tier 1 = %d, %v, %v; want 6", capacity, found, err)
	}
	if _, found, _ := service.DesignSlotCapacity(ctx, "design_fixture", 1, 4); found {
		t.Fatal("tier 4 reported a capacity the projection does not author")
	}
	bundles.versions[3] = domain.AssetBundle{BundleID: "bundle_design", Version: 3, MinAppVersion: 1}
	if _, found, _ := service.DesignSlotCapacity(ctx, "design_fixture", 1, 1); found {
		t.Fatal("a version with no projection reported a capacity")
	}
	delete(bundles.versions, 3)
	if _, found, _ := service.DesignSlotCapacity(ctx, "design_nope", 1, 1); found {
		t.Fatal("an unknown Design reported a capacity")
	}
	production := application.NewCollectionDesignService(designs, true).WithBundleRegistry(bundles)
	if _, found, _ := production.DesignSlotCapacity(ctx, "design_fixture", 1, 1); found {
		t.Fatal("a fixture Design reported a capacity in production")
	}
	unwired := application.NewCollectionDesignService(designs, false)
	if _, found, _ := unwired.DesignSlotCapacity(ctx, "design_fixture", 1, 1); found {
		t.Fatal("a service with no bundle registry reported a capacity")
	}
}

type slotCapacityBundleRepo struct {
	versions map[int]domain.AssetBundle
}

func (r *slotCapacityBundleRepo) ResolveForApp(_ context.Context, bundleID string, appAssetVersion int) (domain.AssetBundle, error) {
	best, found := domain.AssetBundle{}, false
	for _, bundle := range r.versions {
		if bundle.BundleID != bundleID || bundle.MinAppVersion > appAssetVersion {
			continue
		}
		if !found || bundle.Version > best.Version {
			best, found = bundle, true
		}
	}
	if !found {
		return domain.AssetBundle{}, domain.ErrBundleNotFound
	}
	return best, nil
}

func (r *slotCapacityBundleRepo) FindVersion(context.Context, string, int) (domain.AssetBundle, error) {
	return domain.AssetBundle{}, domain.ErrBundleNotFound
}
func (r *slotCapacityBundleRepo) Register(context.Context, domain.AssetBundle) error { return nil }
func (r *slotCapacityBundleRepo) RegisterTierCapacities(context.Context, string, int, domain.TierCapacities) error {
	return nil
}
func (r *slotCapacityBundleRepo) DesignTierCountsNaming(context.Context, string) ([]int, error) {
	return nil, nil
}

type fakeDesigns struct {
	designs map[string]domain.CollectionDesign
}

func newFakeDesigns() *fakeDesigns {
	return &fakeDesigns{designs: map[string]domain.CollectionDesign{}}
}

func (f *fakeDesigns) ListCollectionDesigns(context.Context) ([]domain.CollectionDesign, error) {
	out := make([]domain.CollectionDesign, 0, len(f.designs))
	for _, design := range f.designs {
		out = append(out, design)
	}
	return out, nil
}

func (f *fakeDesigns) FindCollectionDesign(_ context.Context, designID string) (domain.CollectionDesign, bool, error) {
	design, ok := f.designs[designID]
	return design, ok, nil
}

func (f *fakeDesigns) CollectionCategoryExists(context.Context, string) (bool, error) {
	return true, nil
}
