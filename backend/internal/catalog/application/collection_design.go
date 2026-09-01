package application

import (
	"context"
	"errors"
	"fmt"

	"muse-backend/internal/catalog/domain"
)

type CollectionDesignReading interface {
	ListCollectionDesigns(ctx context.Context) ([]domain.CollectionDesign, error)
	FindCollectionDesign(ctx context.Context, designID string) (domain.CollectionDesign, bool, error)
	CollectionCategoryExists(ctx context.Context, categoryID string) (bool, error)
}

type CollectionDesignService struct {
	designs      CollectionDesignReading
	bundles      BundleRepository
	isProduction bool
}

func NewCollectionDesignService(designs CollectionDesignReading, isProduction bool) *CollectionDesignService {
	return &CollectionDesignService{designs: designs, isProduction: isProduction}
}

func (s *CollectionDesignService) WithBundleRegistry(bundles BundleRepository) *CollectionDesignService {
	s.bundles = bundles
	return s
}

func (s *CollectionDesignService) DesignSlotCapacity(
	ctx context.Context,
	designID string,
	appAssetVersion int,
	tier int,
) (capacity int, found bool, err error) {
	if s.bundles == nil || appAssetVersion < 1 {
		return 0, false, nil
	}
	design, found, err := s.designs.FindCollectionDesign(ctx, designID)
	if err != nil {
		return 0, false, fmt.Errorf("catalog: find collection design: %w", err)
	}
	if !found || (s.isProduction && design.IsDevelopmentFixture()) {
		return 0, false, nil
	}
	bundle, err := s.bundles.ResolveForApp(ctx, design.AssetBundle.ID, appAssetVersion)
	if err != nil {
		if errors.Is(err, domain.ErrBundleNotFound) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("catalog: resolve design bundle: %w", err)
	}
	capacity, ok := bundle.TierCapacities.SlotCapacityAt(tier)
	if !ok {
		return 0, false, nil
	}
	return capacity, true, nil
}

func (s *CollectionDesignService) ApplicableDesigns(
	ctx context.Context,
	categoryID string,
) ([]domain.CollectionDesign, error) {
	if categoryID == "" {
		return nil, domain.ErrDesignCategoryRequired
	}
	known, err := s.designs.CollectionCategoryExists(ctx, categoryID)
	if err != nil {
		return nil, fmt.Errorf("catalog: look up category: %w", err)
	}
	if !known {
		return nil, domain.ErrDesignUnknownCategory
	}

	all, err := s.designs.ListCollectionDesigns(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: list collection designs: %w", err)
	}

	applicable := make([]domain.CollectionDesign, 0, len(all))
	for _, design := range all {
		if !design.AppliesTo(categoryID) {
			continue
		}
		if s.isProduction && design.IsDevelopmentFixture() {
			continue
		}
		applicable = append(applicable, design)
	}
	return applicable, nil
}

func (s *CollectionDesignService) IsDesignApplicable(
	ctx context.Context,
	designID string,
	categoryID string,
) (bool, error) {
	design, found, err := s.designs.FindCollectionDesign(ctx, designID)
	if err != nil {
		return false, fmt.Errorf("catalog: find collection design: %w", err)
	}
	if !found {
		return false, nil
	}
	if s.isProduction && design.IsDevelopmentFixture() {
		return false, nil
	}
	return design.AppliesTo(categoryID), nil
}

func (s *CollectionDesignService) DesignTierBound(
	ctx context.Context,
	designID string,
) (tierCount int, found bool, err error) {
	design, found, err := s.designs.FindCollectionDesign(ctx, designID)
	if err != nil {
		return 0, false, fmt.Errorf("catalog: find collection design: %w", err)
	}
	if !found {
		return 0, false, nil
	}
	if s.isProduction && design.IsDevelopmentFixture() {
		return 0, false, nil
	}
	return design.HighestTier(), true, nil
}
