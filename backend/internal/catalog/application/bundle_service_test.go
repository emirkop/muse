package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"muse-backend/internal/catalog/application"
	"muse-backend/internal/catalog/domain"
)

// MARK: - Fakes

type fakeBundleRepo struct {
	byVersion        map[string]domain.AssetBundle
	registered       []domain.AssetBundle
	pointedAt        map[string]int
	designTierCounts map[string][]int
	backfilled       []string
}

func newFakeBundleRepo() *fakeBundleRepo {
	return &fakeBundleRepo{byVersion: map[string]domain.AssetBundle{}, pointedAt: map[string]int{}}
}

func key(bundleID string, version int) string { return fmt.Sprintf("%s@%d", bundleID, version) }

func (r *fakeBundleRepo) ResolveForApp(_ context.Context, bundleID string, appAssetVersion int) (domain.AssetBundle, error) {
	best := domain.AssetBundle{}
	found := false
	for _, bundle := range r.byVersion {
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

func (r *fakeBundleRepo) FindVersion(_ context.Context, bundleID string, version int) (domain.AssetBundle, error) {
	bundle, ok := r.byVersion[key(bundleID, version)]
	if !ok {
		return domain.AssetBundle{}, domain.ErrBundleNotFound
	}
	return bundle, nil
}

func (r *fakeBundleRepo) Register(_ context.Context, bundle domain.AssetBundle) error {
	r.byVersion[key(bundle.BundleID, bundle.Version)] = bundle
	r.registered = append(r.registered, bundle)
	r.pointedAt[bundle.BundleID] = bundle.Version
	return nil
}

func (r *fakeBundleRepo) RegisterTierCapacities(_ context.Context, bundleID string, version int, capacities domain.TierCapacities) error {
	bundle, ok := r.byVersion[key(bundleID, version)]
	if !ok {
		return domain.ErrBundleNotFound
	}
	if len(bundle.TierCapacities) > 0 {
		return fmt.Errorf("%w: projection already registered", domain.ErrBundleVersionImmutable)
	}
	bundle.TierCapacities = capacities
	r.byVersion[key(bundleID, version)] = bundle
	r.backfilled = append(r.backfilled, key(bundleID, version))
	return nil
}

func (r *fakeBundleRepo) DesignTierCountsNaming(_ context.Context, bundleID string) ([]int, error) {
	return r.designTierCounts[bundleID], nil
}

type fakeBundleStore struct {
	put          map[string][]byte
	stat         map[string]application.StoredObject
	putErr       error
	statOverride *application.StoredObject
}

func newFakeBundleStore() *fakeBundleStore {
	return &fakeBundleStore{put: map[string][]byte{}, stat: map[string]application.StoredObject{}}
}

func (s *fakeBundleStore) Put(_ context.Context, key, _ string, body io.Reader, size int64, checksum string) error {
	if s.putErr != nil {
		return s.putErr
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.put[key] = raw
	s.stat[key] = application.StoredObject{ByteSize: size, ChecksumSHA256: checksum}
	return nil
}

func (s *fakeBundleStore) Stat(_ context.Context, key string) (application.StoredObject, error) {
	if s.statOverride != nil {
		return *s.statOverride, nil
	}
	stat, ok := s.stat[key]
	if !ok {
		return application.StoredObject{}, errors.New("not found")
	}
	return stat, nil
}

func (s *fakeBundleStore) PublicURL(key string) string { return "https://assets.example/" + key }

type stringSource struct{ *strings.Reader }

func (s stringSource) Close() error { return nil }

func publishSource(assetID string, role domain.AssetRole, body string) application.PublishSource {
	return application.PublishSource{
		AssetID:        assetID,
		Role:           role,
		ContentType:    "application/octet-stream",
		ByteSize:       int64(len(body)),
		ChecksumSHA256: strings.Repeat("a", 64),
		Open: func() (application.ReadSeekCloser, error) {
			return stringSource{strings.NewReader(body)}, nil
		},
	}
}

func validRequest(bundleID string, version int) application.PublishRequest {
	return application.PublishRequest{
		BundleID:      bundleID,
		Version:       version,
		Kind:          domain.BundleKindRoomVariant,
		Format:        "usda",
		MinAppVersion: 1,
		Files: []application.PublishSource{
			publishSource("geometry", domain.RoleGeometry, "geometry bytes"),
			publishSource("layout", domain.RoleLayout, "{}"),
		},
	}
}

// MARK: - Manifest resolution

func TestManifest_ResolvesPublishedVersionWithFetchableFiles(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	publisher := application.NewBundlePublisher(repo, store)
	if _, err := publisher.Publish(context.Background(), validRequest("b", 1)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	manifest, err := application.NewBundleService(repo, store).Manifest(context.Background(), "b", 1)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if manifest.Version != 1 || manifest.Format != "usda" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(manifest.Files))
	}
	if manifest.Files[0].Role != domain.RoleGeometry {
		t.Errorf("expected geometry first, got %q", manifest.Files[0].Role)
	}
	for _, file := range manifest.Files {
		if file.URL != "https://assets.example/bundles/b/v1/"+file.AssetID {
			t.Errorf("unexpected URL for %s: %s", file.AssetID, file.URL)
		}
		if len(file.ChecksumSHA256) != 64 || file.ByteSize <= 0 {
			t.Errorf("file %s has no usable integrity metadata: %+v", file.AssetID, file)
		}
	}
}

func TestManifest_UnpublishedBundleIsNotFound(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	_, err := application.NewBundleService(repo, store).Manifest(context.Background(), "nothing", 1)
	if !errors.Is(err, domain.ErrBundleNotFound) {
		t.Fatalf("expected ErrBundleNotFound, got %v", err)
	}
}

func TestManifest_WithoutAStoreReportsUnconfigured(t *testing.T) {
	_, err := application.NewBundleService(newFakeBundleRepo(), nil).Manifest(context.Background(), "b", 1)
	if !errors.Is(err, application.ErrBundleDeliveryUnconfigured) {
		t.Fatalf("expected ErrBundleDeliveryUnconfigured, got %v", err)
	}
}

func TestManifest_NewerVersionSupersedesOlder(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	publisher := application.NewBundlePublisher(repo, store)
	ctx := context.Background()

	if _, err := publisher.Publish(ctx, validRequest("b", 1)); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	service := application.NewBundleService(repo, store)
	first, err := service.Manifest(ctx, "b", 1)
	if err != nil || first.Version != 1 {
		t.Fatalf("expected v1, got %+v (%v)", first, err)
	}

	second := validRequest("b", 2)
	second.Files[0] = publishSource("geometry", domain.RoleGeometry, "different geometry bytes")
	if _, err := publisher.Publish(ctx, second); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	current, err := service.Manifest(ctx, "b", 1)
	if err != nil {
		t.Fatalf("manifest after v2: %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("expected v2 to be current, got v%d", current.Version)
	}
	if current.Files[0].URL == first.Files[0].URL {
		t.Error("v2's geometry URL is identical to v1's — versions are not key-isolated")
	}
	if got := repo.pointedAt["b"]; got != 2 {
		t.Errorf("presentation rows were not re-pointed at v2 (got v%d)", got)
	}
}

func TestManifest_OldClientGetsNewestCompatibleNotNewest(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	publisher := application.NewBundlePublisher(repo, store)
	ctx := context.Background()

	if _, err := publisher.Publish(ctx, validRequest("b", 1)); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	future := validRequest("b", 2)
	future.MinAppVersion = 2
	future.Format = "usdz"
	future.Files[0] = publishSource("geometry", domain.RoleGeometry, "next generation")
	if _, err := publisher.Publish(ctx, future); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	service := application.NewBundleService(repo, store)
	old, err := service.Manifest(ctx, "b", 1)
	if err != nil {
		t.Fatalf("old client manifest: %v", err)
	}
	if old.Version != 1 {
		t.Errorf("an app at asset version 1 must receive v1, got v%d", old.Version)
	}
	updated, err := service.Manifest(ctx, "b", 2)
	if err != nil {
		t.Fatalf("updated client manifest: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("an app at asset version 2 must receive v2, got v%d", updated.Version)
	}
}

// MARK: - Publishing

func TestPublish_UploadsEveryFileAndRegistersTheVersion(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	result, err := application.NewBundlePublisher(repo, store).Publish(context.Background(), validRequest("b", 1))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if result.AlreadyPublished {
		t.Error("a first publish must not report AlreadyPublished")
	}
	if len(result.UploadedAssetIDs) != 2 {
		t.Errorf("expected 2 uploads, got %v", result.UploadedAssetIDs)
	}
	for _, assetID := range []string{"geometry", "layout"} {
		if _, ok := store.put["bundles/b/v1/"+assetID]; !ok {
			t.Errorf("no bytes stored for %s", assetID)
		}
	}
	if len(repo.registered) != 1 {
		t.Fatalf("expected one registration, got %d", len(repo.registered))
	}
}

func TestPublish_RepublishingIdenticalContentIsANoOp(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	publisher := application.NewBundlePublisher(repo, store)
	ctx := context.Background()
	if _, err := publisher.Publish(ctx, validRequest("b", 1)); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	result, err := publisher.Publish(ctx, validRequest("b", 1))
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if !result.AlreadyPublished {
		t.Error("expected AlreadyPublished")
	}
	if len(result.UploadedAssetIDs) != 0 {
		t.Errorf("nothing should have been uploaded, got %v", result.UploadedAssetIDs)
	}
	if len(repo.registered) != 1 {
		t.Errorf("expected still one registration, got %d", len(repo.registered))
	}
}

func TestPublish_RepublishingDifferentContentIsRefused(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	publisher := application.NewBundlePublisher(repo, store)
	ctx := context.Background()
	if _, err := publisher.Publish(ctx, validRequest("b", 1)); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	changed := validRequest("b", 1)
	changed.Files[0].ChecksumSHA256 = strings.Repeat("b", 64)
	_, err := publisher.Publish(ctx, changed)
	if !errors.Is(err, domain.ErrBundleVersionImmutable) {
		t.Fatalf("expected ErrBundleVersionImmutable, got %v", err)
	}
}

func TestPublish_RefusesWhenStorageReportsDifferentBytes(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	store.statOverride = &application.StoredObject{ByteSize: 3, ChecksumSHA256: strings.Repeat("a", 64)}

	_, err := application.NewBundlePublisher(repo, store).Publish(context.Background(), validRequest("b", 1))
	if !errors.Is(err, domain.ErrBundleInvalid) {
		t.Fatalf("expected ErrBundleInvalid for a size mismatch, got %v", err)
	}
	if len(repo.registered) != 0 {
		t.Error("a bundle whose stored bytes disagree must not be registered")
	}
}

func TestPublish_RefusesWhenStorageReportsADifferentChecksum(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	store.statOverride = &application.StoredObject{ByteSize: int64(len("geometry bytes")), ChecksumSHA256: strings.Repeat("c", 64)}

	_, err := application.NewBundlePublisher(repo, store).Publish(context.Background(), validRequest("b", 1))
	if !errors.Is(err, domain.ErrBundleInvalid) {
		t.Fatalf("expected ErrBundleInvalid for a checksum mismatch, got %v", err)
	}
}

func TestPublish_AcceptsAStoreThatCannotReportAChecksum(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	store.statOverride = &application.StoredObject{ByteSize: int64(len("geometry bytes")), ChecksumSHA256: ""}

	request := validRequest("b", 1)
	request.Files[1] = publishSource("layout", domain.RoleLayout, "geometry bytes")
	if _, err := application.NewBundlePublisher(repo, store).Publish(context.Background(), request); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func TestPublish_RefusesAnInvalidBundleBeforeTouchingStorage(t *testing.T) {
	cases := map[string]func(*application.PublishRequest){
		"no files":     func(r *application.PublishRequest) { r.Files = nil },
		"no geometry":  func(r *application.PublishRequest) { r.Files = r.Files[1:] },
		"unknown kind": func(r *application.PublishRequest) { r.Kind = "not_a_bundle_kind" },
		"unknown role": func(r *application.PublishRequest) { r.Files[1].Role = "sound" },
		"zero version": func(r *application.PublishRequest) { r.Version = 0 },
		"bad checksum": func(r *application.PublishRequest) { r.Files[0].ChecksumSHA256 = "short" },
		"self dependency": func(r *application.PublishRequest) {
			r.Dependencies = []domain.BundleDependency{{BundleID: "b", Version: 1}}
		},
		"duplicate assets": func(r *application.PublishRequest) { r.Files[1].AssetID = "geometry" },
		"two geometries":   func(r *application.PublishRequest) { r.Files[1].Role = domain.RoleGeometry },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			repo, store := newFakeBundleRepo(), newFakeBundleStore()
			request := validRequest("b", 1)
			mutate(&request)
			_, err := application.NewBundlePublisher(repo, store).Publish(context.Background(), request)
			if !errors.Is(err, domain.ErrBundleInvalid) {
				t.Fatalf("expected ErrBundleInvalid, got %v", err)
			}
			if len(store.put) != 0 {
				t.Error("nothing may be uploaded for an invalid bundle")
			}
		})
	}
}

func TestBundleIdentity_IsAddressableWithoutAnyUserContent(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	publisher := application.NewBundlePublisher(repo, store)
	ctx := context.Background()
	if _, err := publisher.Publish(ctx, validRequest("b", 7)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	for key := range store.put {
		if !strings.HasPrefix(key, "bundles/b/v7/") {
			t.Errorf("unexpected storage key %q", key)
		}
	}
	manifest, err := application.NewBundleService(repo, store).Manifest(ctx, "b", 1)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if manifest.BundleID != "b" || manifest.Version != 7 {
		t.Fatalf("unexpected identity: %s v%d", manifest.BundleID, manifest.Version)
	}
}

// MARK: -: the derived tier-capacity projection

func designLayout(tiers string) string {
	return `{"format_version":1,"design_id":"dev-fixture:collection-design","entry":{"position":[0,0,1]},"tiers":[` + tiers + `]}`
}

func designTier(ordinal, cumulative int, slots ...int) string {
	var transforms []string
	for _, slot := range slots {
		transforms = append(transforms, fmt.Sprintf(`{"slot_index":%d,"position":[0,0,0]}`, slot))
	}
	return fmt.Sprintf(`{"tier":%d,"cumulative_capacity":%d,"item_transforms":[%s]}`,
		ordinal, cumulative, strings.Join(transforms, ","))
}

const fixtureShapedLayout = ""

func fixtureLayout() string {
	return designLayout(
		designTier(1, 4, 0, 1, 2, 3) + "," +
			designTier(2, 10, 4, 5, 6, 7, 8, 9) + "," +
			designTier(3, 18, 10, 11, 12, 13, 14, 15, 16, 17),
	)
}

func designRequest(bundleID string, version int, layout string) application.PublishRequest {
	request := application.PublishRequest{
		BundleID: bundleID, Version: version, Kind: domain.BundleKindCollectionDesign,
		Format: "usda", MinAppVersion: 1,
		Files: []application.PublishSource{
			publishSource("geometry", domain.RoleGeometry, "#usda 1.0"),
		},
	}
	if layout != "" {
		request.Files = append(request.Files, publishSource("layout", domain.RoleLayout, layout))
	}
	return request
}

var fixtureCapacities = domain.TierCapacities{{Tier: 1, Cumulative: 4}, {Tier: 2, Cumulative: 10}, {Tier: 3, Cumulative: 18}}

func TestPublish_DerivesTierCapacitiesFromADesignLayout(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	result, err := application.NewBundlePublisher(repo, store).Publish(context.Background(),
		designRequest("dev_fixture_collection_design", 1, fixtureLayout()))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !result.Bundle.TierCapacities.Equal(fixtureCapacities) {
		t.Fatalf("derived %v, want %v", result.Bundle.TierCapacities, fixtureCapacities)
	}
	if len(repo.registered) != 1 || !repo.registered[0].TierCapacities.Equal(fixtureCapacities) {
		t.Fatalf("the registered bundle carries %v", repo.registered)
	}
	if string(store.put["bundles/dev_fixture_collection_design/v1/layout"]) != fixtureLayout() {
		t.Fatal("the stored layout is not the layout the projection was derived from")
	}
}

func TestPublish_RefusesMalformedDesignTierMetadataBeforeTouchingStorage(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		layout string
	}{
		{"no tiers", designLayout("")},
		{"non-monotonic", designLayout(designTier(1, 4, 0, 1, 2, 3) + "," + designTier(2, 4))},
		{"non-positive", designLayout(designTier(1, 0))},
		{"duplicate ordinal", designLayout(designTier(1, 1, 0) + "," + designTier(1, 2, 1))},
		{"gap in ordinals", designLayout(designTier(1, 1, 0) + "," + designTier(3, 2, 1))},
		{"slots disagree with capacity", designLayout(designTier(1, 4, 0, 1))},
		{"not JSON", "{"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo, store := newFakeBundleRepo(), newFakeBundleStore()
			_, err := application.NewBundlePublisher(repo, store).Publish(context.Background(),
				designRequest("dev_fixture_collection_design", 1, testCase.layout))
			if !errors.Is(err, domain.ErrBundleInvalid) {
				t.Fatalf("got %v, want ErrBundleInvalid", err)
			}
			if len(store.put) != 0 {
				t.Fatalf("bytes were uploaded for a refused bundle: %v", store.put)
			}
			if len(repo.registered) != 0 {
				t.Fatal("a refused bundle was registered")
			}
		})
	}
}

func TestPublish_RefusesALayoutDisagreeingWithTheDesignsTierCount(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	repo.designTierCounts = map[string][]int{"dev_fixture_collection_design": {2}}

	_, err := application.NewBundlePublisher(repo, store).Publish(context.Background(),
		designRequest("dev_fixture_collection_design", 1, fixtureLayout()))
	if !errors.Is(err, domain.ErrBundleInvalid) {
		t.Fatalf("got %v, want ErrBundleInvalid", err)
	}
	if len(store.put) != 0 || len(repo.registered) != 0 {
		t.Fatal("a disagreeing bundle was uploaded or registered")
	}

	repo.designTierCounts["dev_fixture_collection_design"] = []int{3}
	if _, err := application.NewBundlePublisher(repo, store).Publish(context.Background(),
		designRequest("dev_fixture_collection_design", 1, fixtureLayout())); err != nil {
		t.Fatalf("an agreeing publish was refused: %v", err)
	}
}

func TestPublish_ABaseDesignBundleMustCarryALayoutButATierGeometryBundleNeedNot(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	repo.designTierCounts = map[string][]int{"dev_fixture_collection_design": {3}}
	publisher := application.NewBundlePublisher(repo, store)
	ctx := context.Background()

	_, err := publisher.Publish(ctx, designRequest("dev_fixture_collection_design", 1, ""))
	if !errors.Is(err, domain.ErrBundleInvalid) {
		t.Fatalf("a base Design bundle with no layout published: %v", err)
	}

	result, err := publisher.Publish(ctx, designRequest("dev_fixture_collection_design_tier2", 1, ""))
	if err != nil {
		t.Fatalf("a tier-geometry bundle was refused: %v", err)
	}
	if len(result.Bundle.TierCapacities) != 0 {
		t.Fatalf("a geometry-only bundle derived capacities: %v", result.Bundle.TierCapacities)
	}
}

func TestPublish_DoesNotDeriveTierCapacitiesFromARoomVariantLayout(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	result, err := application.NewBundlePublisher(repo, store).Publish(context.Background(), validRequest("variant", 1))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(result.Bundle.TierCapacities) != 0 {
		t.Fatalf("a room_variant derived tier capacities: %v", result.Bundle.TierCapacities)
	}
}

func TestPublish_IdempotentRerunBackfillsAMissingProjection(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	publisher := application.NewBundlePublisher(repo, store)
	ctx := context.Background()

	first, err := publisher.Publish(ctx, designRequest("dev_fixture_collection_design", 1, fixtureLayout()))
	if err != nil {
		t.Fatal(err)
	}
	stripped := repo.byVersion[key("dev_fixture_collection_design", 1)]
	stripped.TierCapacities = nil
	repo.byVersion[key("dev_fixture_collection_design", 1)] = stripped

	result, err := publisher.Publish(ctx, designRequest("dev_fixture_collection_design", 1, fixtureLayout()))
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if !result.AlreadyPublished || !result.ProjectionBackfilled {
		t.Fatalf("expected AlreadyPublished + ProjectionBackfilled, got %+v", result)
	}
	if len(result.UploadedAssetIDs) != 0 {
		t.Fatalf("a re-run uploaded bytes: %v", result.UploadedAssetIDs)
	}
	if !result.Bundle.TierCapacities.Equal(first.Bundle.TierCapacities) {
		t.Fatalf("backfilled %v, want %v", result.Bundle.TierCapacities, first.Bundle.TierCapacities)
	}
	if len(repo.backfilled) != 1 {
		t.Fatalf("backfilled versions: %v", repo.backfilled)
	}

	again, err := publisher.Publish(ctx, designRequest("dev_fixture_collection_design", 1, fixtureLayout()))
	if err != nil {
		t.Fatal(err)
	}
	if again.ProjectionBackfilled || len(repo.backfilled) != 1 {
		t.Fatal("a second re-run backfilled again")
	}
}

func TestPublish_ProjectionCannotBeChangedByRepublishing(t *testing.T) {
	repo, store := newFakeBundleRepo(), newFakeBundleStore()
	publisher := application.NewBundlePublisher(repo, store)
	ctx := context.Background()
	if _, err := publisher.Publish(ctx, designRequest("dev_fixture_collection_design", 1, fixtureLayout())); err != nil {
		t.Fatal(err)
	}

	changed := designLayout(designTier(1, 5, 0, 1, 2, 3, 4) + "," + designTier(2, 10, 5, 6, 7, 8, 9) + "," + designTier(3, 18, 10, 11, 12, 13, 14, 15, 16, 17))
	_, err := publisher.Publish(ctx, designRequest("dev_fixture_collection_design", 1, changed))
	if !errors.Is(err, domain.ErrBundleVersionImmutable) {
		t.Fatalf("a changed layout under the same version: %v, want ErrBundleVersionImmutable", err)
	}
	if !repo.byVersion[key("dev_fixture_collection_design", 1)].TierCapacities.Equal(fixtureCapacities) {
		t.Fatal("the projection changed")
	}

	tampered := repo.byVersion[key("dev_fixture_collection_design", 1)]
	tampered.TierCapacities = domain.TierCapacities{{Tier: 1, Cumulative: 99}}
	repo.byVersion[key("dev_fixture_collection_design", 1)] = tampered
	_, err = publisher.Publish(ctx, designRequest("dev_fixture_collection_design", 1, fixtureLayout()))
	if !errors.Is(err, domain.ErrBundleVersionImmutable) {
		t.Fatalf("a disagreeing projection: %v, want ErrBundleVersionImmutable", err)
	}
}
