package application

import (
	"context"
	"errors"
	"fmt"
	"io"

	"muse-backend/internal/catalog/domain"
)

type BundleManifest struct {
	BundleID      string
	Version       int
	Kind          domain.BundleKind
	Format        string
	MinAppVersion int
	Files         []ManifestFile
	Dependencies  []domain.BundleDependency
}

type ManifestFile struct {
	AssetID        string
	Role           domain.AssetRole
	URL            string
	ContentType    string
	ByteSize       int64
	ChecksumSHA256 string
}

var ErrBundleDeliveryUnconfigured = errors.New("catalog: asset bundle delivery is not configured")

type BundleService struct {
	bundles BundleRepository
	store   BundleObjectStore
}

func NewBundleService(bundles BundleRepository, store BundleObjectStore) *BundleService {
	return &BundleService{bundles: bundles, store: store}
}

func (s *BundleService) Manifest(ctx context.Context, bundleID string, appAssetVersion int) (BundleManifest, error) {
	if s.store == nil {
		return BundleManifest{}, ErrBundleDeliveryUnconfigured
	}
	if appAssetVersion <= 0 {
		return BundleManifest{}, fmt.Errorf("%w: app asset version must be positive", domain.ErrBundleInvalid)
	}

	bundle, err := s.bundles.ResolveForApp(ctx, bundleID, appAssetVersion)
	if err != nil {
		return BundleManifest{}, err
	}

	ordered := bundle.OrderedFiles()
	files := make([]ManifestFile, 0, len(ordered))
	for _, file := range ordered {
		files = append(files, ManifestFile{
			AssetID:        file.AssetID,
			Role:           file.Role,
			URL:            s.store.PublicURL(file.StorageKey),
			ContentType:    file.ContentType,
			ByteSize:       file.ByteSize,
			ChecksumSHA256: file.ChecksumSHA256,
		})
	}

	return BundleManifest{
		BundleID:      bundle.BundleID,
		Version:       bundle.Version,
		Kind:          bundle.Kind,
		Format:        bundle.Format,
		MinAppVersion: bundle.MinAppVersion,
		Files:         files,
		Dependencies:  bundle.Dependencies,
	}, nil
}

// MARK: - Publishing

type PublishSource struct {
	AssetID        string
	Role           domain.AssetRole
	ContentType    string
	ByteSize       int64
	ChecksumSHA256 string
	Open           func() (ReadSeekCloser, error)
}

type ReadSeekCloser interface {
	Read(p []byte) (int, error)
	Close() error
}

type PublishRequest struct {
	BundleID      string
	Version       int
	Kind          domain.BundleKind
	Format        string
	MinAppVersion int
	Files         []PublishSource
	Dependencies  []domain.BundleDependency
}

type PublishResult struct {
	Bundle               domain.AssetBundle
	AlreadyPublished     bool
	UploadedAssetIDs     []string
	ProjectionBackfilled bool
}

type BundlePublisher struct {
	bundles BundleRepository
	store   BundleObjectStore
}

func NewBundlePublisher(bundles BundleRepository, store BundleObjectStore) *BundlePublisher {
	return &BundlePublisher{bundles: bundles, store: store}
}

func (p *BundlePublisher) Publish(ctx context.Context, request PublishRequest) (PublishResult, error) {
	bundle := domain.AssetBundle{
		BundleID:      request.BundleID,
		Version:       request.Version,
		Kind:          request.Kind,
		Format:        request.Format,
		MinAppVersion: request.MinAppVersion,
		Dependencies:  request.Dependencies,
	}
	for _, source := range request.Files {
		bundle.Files = append(bundle.Files, domain.BundleFile{
			AssetID:        source.AssetID,
			Role:           source.Role,
			StorageKey:     domain.StorageKeyFor(request.BundleID, request.Version, source.AssetID),
			ContentType:    source.ContentType,
			ByteSize:       source.ByteSize,
			ChecksumSHA256: source.ChecksumSHA256,
		})
	}
	if err := p.deriveTierCapacities(ctx, &bundle, request.Files); err != nil {
		return PublishResult{}, err
	}
	if err := bundle.Validate(); err != nil {
		return PublishResult{}, err
	}

	existing, err := p.bundles.FindVersion(ctx, bundle.BundleID, bundle.Version)
	switch {
	case err == nil:
		if err := assertSameContent(existing, bundle); err != nil {
			return PublishResult{}, err
		}
		switch {
		case len(bundle.TierCapacities) == 0:
			return PublishResult{Bundle: existing, AlreadyPublished: true}, nil
		case len(existing.TierCapacities) == 0:
			if err := p.bundles.RegisterTierCapacities(ctx, bundle.BundleID, bundle.Version, bundle.TierCapacities); err != nil {
				return PublishResult{}, err
			}
			existing.TierCapacities = bundle.TierCapacities
			return PublishResult{Bundle: existing, AlreadyPublished: true, ProjectionBackfilled: true}, nil
		case !existing.TierCapacities.Equal(bundle.TierCapacities):
			return PublishResult{}, fmt.Errorf("%w: %s v%d's registered tier capacities differ from those derived from its layout — the derivation changed; review it rather than re-publishing",
				domain.ErrBundleVersionImmutable, bundle.BundleID, bundle.Version)
		default:
			return PublishResult{Bundle: existing, AlreadyPublished: true}, nil
		}
	case errors.Is(err, domain.ErrBundleNotFound):
	default:
		return PublishResult{}, err
	}

	sourcesByAssetID := make(map[string]PublishSource, len(request.Files))
	for _, source := range request.Files {
		sourcesByAssetID[source.AssetID] = source
	}

	uploaded := make([]string, 0, len(bundle.Files))
	for _, file := range bundle.Files {
		source := sourcesByAssetID[file.AssetID]
		if err := p.upload(ctx, file, source); err != nil {
			return PublishResult{}, err
		}
		uploaded = append(uploaded, file.AssetID)
	}

	if err := p.bundles.Register(ctx, bundle); err != nil {
		return PublishResult{}, err
	}
	return PublishResult{Bundle: bundle, UploadedAssetIDs: uploaded}, nil
}

func (p *BundlePublisher) deriveTierCapacities(ctx context.Context, bundle *domain.AssetBundle, sources []PublishSource) error {
	if bundle.Kind != domain.BundleKindCollectionDesign {
		return nil
	}
	declaredTierCounts, err := p.bundles.DesignTierCountsNaming(ctx, bundle.BundleID)
	if err != nil {
		return err
	}

	var layout *PublishSource
	for index := range sources {
		if sources[index].Role == domain.RoleLayout {
			layout = &sources[index]
			break
		}
	}
	if layout == nil {
		if len(declaredTierCounts) > 0 {
			return fmt.Errorf("%w: %s is a Collection Design's base bundle and must carry a layout with its tier table",
				domain.ErrBundleInvalid, bundle.BundleID)
		}
		return nil
	}

	handle, err := layout.Open()
	if err != nil {
		return fmt.Errorf("catalog: open %s: %w", layout.AssetID, err)
	}
	raw, err := io.ReadAll(io.LimitReader(handle, domain.MaxDesignLayoutBytes+1))
	closeErr := handle.Close()
	if err != nil {
		return fmt.Errorf("catalog: read %s: %w", layout.AssetID, err)
	}
	if closeErr != nil {
		return fmt.Errorf("catalog: close %s: %w", layout.AssetID, closeErr)
	}

	_, capacities, err := domain.ParseCollectionDesignTierCapacities(raw)
	if err != nil {
		return err
	}
	for _, declared := range declaredTierCounts {
		if declared != len(capacities) {
			return fmt.Errorf("%w: %s's layout authors %d tiers but a Collection Design naming it declares tier_count %d — one of them is wrong",
				domain.ErrBundleInvalid, bundle.BundleID, len(capacities), declared)
		}
	}
	bundle.TierCapacities = capacities
	return nil
}

func (p *BundlePublisher) upload(ctx context.Context, file domain.BundleFile, source PublishSource) error {
	handle, err := source.Open()
	if err != nil {
		return fmt.Errorf("catalog: open %s: %w", file.AssetID, err)
	}
	err = p.store.Put(ctx, file.StorageKey, file.ContentType, handle, file.ByteSize, file.ChecksumSHA256)
	closeErr := handle.Close()
	if err != nil {
		return fmt.Errorf("catalog: upload %s: %w", file.AssetID, err)
	}
	if closeErr != nil {
		return fmt.Errorf("catalog: close %s: %w", file.AssetID, closeErr)
	}

	stored, err := p.store.Stat(ctx, file.StorageKey)
	if err != nil {
		return fmt.Errorf("catalog: verify %s: %w", file.AssetID, err)
	}
	if stored.ByteSize != file.ByteSize {
		return fmt.Errorf("%w: %s stored %d bytes, expected %d",
			domain.ErrBundleInvalid, file.AssetID, stored.ByteSize, file.ByteSize)
	}
	if stored.ChecksumSHA256 != "" && stored.ChecksumSHA256 != file.ChecksumSHA256 {
		return fmt.Errorf("%w: %s stored checksum %s, expected %s",
			domain.ErrBundleInvalid, file.AssetID, stored.ChecksumSHA256, file.ChecksumSHA256)
	}
	return nil
}

func assertSameContent(existing, incoming domain.AssetBundle) error {
	existingChecksums := make(map[string]string, len(existing.Files))
	for _, file := range existing.Files {
		existingChecksums[file.AssetID] = file.ChecksumSHA256
	}
	if len(existingChecksums) != len(incoming.Files) {
		return fmt.Errorf("%w: %s v%d already has %d files, this publish has %d",
			domain.ErrBundleVersionImmutable, existing.BundleID, existing.Version,
			len(existingChecksums), len(incoming.Files))
	}
	for _, file := range incoming.Files {
		published, ok := existingChecksums[file.AssetID]
		if !ok {
			return fmt.Errorf("%w: %s v%d does not contain asset %q",
				domain.ErrBundleVersionImmutable, existing.BundleID, existing.Version, file.AssetID)
		}
		if published != file.ChecksumSHA256 {
			return fmt.Errorf("%w: %s v%d asset %q differs (published %s, incoming %s) — publish a new version instead",
				domain.ErrBundleVersionImmutable, existing.BundleID, existing.Version,
				file.AssetID, published, file.ChecksumSHA256)
		}
	}
	return nil
}
