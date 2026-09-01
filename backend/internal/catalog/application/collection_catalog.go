package application

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"muse-backend/internal/catalog/domain"
)

type CollectionCatalogReading interface {
	SearchCollectionModels(ctx context.Context, query domain.ModelSearchQuery) (domain.ModelSearchPage, error)
	CollectionCategoryExists(ctx context.Context, categoryID string) (bool, error)
	FindCollectionModel(ctx context.Context, modelID string) (domain.CollectionModel, bool, error)
	FindCollectionModels(ctx context.Context, modelIDs []string) ([]domain.CollectionModel, error)
}

const (
	DefaultModelSearchLimit    = 25
	MaxModelSearchLimit        = 100
	maxSearchTermLength        = 64
	maxSearchTerms             = 12
	MaxPresentationAssetLookup = 100
)

type CollectionCatalogService struct {
	catalog      CollectionCatalogReading
	isProduction bool
}

func NewCollectionCatalogService(catalog CollectionCatalogReading, isProduction bool) *CollectionCatalogService {
	return &CollectionCatalogService{catalog: catalog, isProduction: isProduction}
}

func (s *CollectionCatalogService) SearchModels(
	ctx context.Context,
	categoryID string,
	rawQuery string,
	limit int,
	cursor *domain.ModelSearchCursor,
) (domain.ModelSearchPage, error) {
	if categoryID == "" {
		return domain.ModelSearchPage{}, domain.ErrSearchCategoryRequired
	}
	known, err := s.catalog.CollectionCategoryExists(ctx, categoryID)
	if err != nil {
		return domain.ModelSearchPage{}, fmt.Errorf("catalog: look up category: %w", err)
	}
	if !known {
		return domain.ModelSearchPage{}, domain.ErrSearchUnknownCategory
	}

	page, err := s.catalog.SearchCollectionModels(ctx, domain.ModelSearchQuery{
		CategoryID: domain.CollectionCategoryID(categoryID),
		Terms:      SearchTerms(rawQuery),
		Limit:      clampLimit(limit),
		Cursor:     cursor,
	})
	if err != nil {
		return domain.ModelSearchPage{}, fmt.Errorf("catalog: search collection models: %w", err)
	}

	if s.isProduction {
		kept := make([]domain.CollectionModel, 0, len(page.Models))
		for _, model := range page.Models {
			if model.IsDevelopmentFixture() {
				continue
			}
			kept = append(kept, model)
		}
		page.Models = kept
	}
	return page, nil
}

func (s *CollectionCatalogService) IsCollectionModelPlaceable(
	ctx context.Context,
	modelID string,
	categoryID string,
) (bool, error) {
	if modelID == "" || categoryID == "" {
		return false, nil
	}
	model, found, err := s.catalog.FindCollectionModel(ctx, modelID)
	if err != nil {
		return false, fmt.Errorf("catalog: find collection model: %w", err)
	}
	if !found {
		return false, nil
	}
	if s.isProduction && model.IsDevelopmentFixture() {
		return false, nil
	}
	return string(model.CategoryID) == categoryID, nil
}

func (s *CollectionCatalogService) PresentationAssetMappings(
	ctx context.Context,
	modelIDs []string,
) ([]domain.PresentationAssetMapping, error) {
	unique := make([]string, 0, len(modelIDs))
	seen := make(map[string]struct{}, len(modelIDs))
	for _, id := range modelIDs {
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil, domain.ErrPresentationAssetIDsRequired
	}
	if len(unique) > MaxPresentationAssetLookup {
		return nil, domain.ErrPresentationAssetTooManyIDs
	}

	models, err := s.catalog.FindCollectionModels(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("catalog: find collection models: %w", err)
	}

	mappings := make([]domain.PresentationAssetMapping, 0, len(models))
	for _, model := range models {
		if s.isProduction && model.IsDevelopmentFixture() {
			continue
		}
		mappings = append(mappings, domain.PresentationAssetMappingFor(model))
	}
	return mappings, nil
}

func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultModelSearchLimit
	case limit > MaxModelSearchLimit:
		return MaxModelSearchLimit
	default:
		return limit
	}
}

func SearchTerms(raw string) []string {
	var (
		terms   []string
		current strings.Builder
	)
	flush := func() {
		if current.Len() == 0 {
			return
		}
		term := current.String()
		if len(term) > maxSearchTermLength {
			term = term[:maxSearchTermLength]
		}
		terms = append(terms, term)
		current.Reset()
	}

	for _, r := range strings.ToLower(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
		if len(terms) >= maxSearchTerms {
			return terms
		}
	}
	flush()
	if len(terms) > maxSearchTerms {
		terms = terms[:maxSearchTerms]
	}
	return terms
}
