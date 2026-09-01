package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/catalog/domain"
	"muse-backend/internal/platform/database"
)

type PostgresCatalogRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCatalogRepository(pool *pgxpool.Pool) *PostgresCatalogRepository {
	return &PostgresCatalogRepository{pool: pool}
}

func (r *PostgresCatalogRepository) EnsureSeeded(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin seed: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	for _, style := range domain.SeedStyles() {
		const query = `
			INSERT INTO museum_styles (id, display_name, asset_bundle_id, asset_bundle_version)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, query, string(style.ID), style.DisplayName, style.AssetBundle.ID, style.AssetBundle.Version); err != nil {
			return fmt.Errorf("catalog: seed style %s: %w", style.ID, err)
		}
	}

	for _, variant := range domain.SeedVariants() {
		const query = `
			INSERT INTO room_variants (id, style_id, display_name, asset_bundle_id, asset_bundle_version)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, query, string(variant.ID), string(variant.StyleID), variant.DisplayName, variant.AssetBundle.ID, variant.AssetBundle.Version); err != nil {
			return fmt.Errorf("catalog: seed variant %s: %w", variant.ID, err)
		}
	}

	for _, sculpture := range domain.SeedSculptures() {
		const query = `
			INSERT INTO sculptures (id, display_name, asset_bundle_id, asset_bundle_version)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, query, string(sculpture.ID), sculpture.DisplayName, sculpture.AssetBundle.ID, sculpture.AssetBundle.Version); err != nil {
			return fmt.Errorf("catalog: seed sculpture %s: %w", sculpture.ID, err)
		}
	}

	for _, track := range domain.SeedMusicTracks() {
		const query = `
			INSERT INTO music_tracks (id, display_name, attribution, licensing, storage_key, content_type, duration_seconds)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, query, string(track.ID), track.DisplayName, track.Attribution,
			string(track.Licensing), track.StorageKey, track.ContentType, track.DurationSeconds); err != nil {
			return fmt.Errorf("catalog: seed music track %s: %w", track.ID, err)
		}
	}

	for _, category := range domain.SeedCollectionCategories() {
		const query = `
			INSERT INTO collection_categories (id, display_name, sort_order)
			VALUES ($1, $2, $3)
			ON CONFLICT (id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, query, string(category.ID), category.DisplayName, category.SortOrder); err != nil {
			return fmt.Errorf("catalog: seed collection category %s: %w", category.ID, err)
		}
	}

	for _, design := range domain.SeedCollectionDesigns() {
		const query = `
			INSERT INTO collection_designs
				(id, category_id, display_name, classification, asset_bundle_id, asset_bundle_version, sort_order, tier_count)
			VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, query, design.ID, design.CategoryID, design.DisplayName,
			string(design.Classification), design.AssetBundle.ID, design.AssetBundle.Version,
			design.SortOrder, design.TierCount); err != nil {
			return fmt.Errorf("catalog: seed collection design %s: %w", design.ID, err)
		}
	}

	for _, brand := range domain.SeedCollectionBrands() {
		const query = `
			INSERT INTO collection_brands (id, display_name, sort_order, classification)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, query, string(brand.ID), brand.DisplayName,
			brand.SortOrder, string(brand.Classification)); err != nil {
			return fmt.Errorf("catalog: seed collection brand %s: %w", brand.ID, err)
		}
	}
	for _, model := range domain.SeedCollectionModels() {
		const query = `
			INSERT INTO collection_models
				(id, brand_id, category_id, display_name, search_text, metadata,
				 asset_bundle_id, asset_bundle_version, classification)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (id) DO NOTHING
		`
		if _, err := tx.Exec(ctx, query, string(model.ID), string(model.BrandID),
			string(model.CategoryID), model.DisplayName, model.SearchText, model.Metadata,
			model.AssetBundle.ID, model.AssetBundle.Version,
			string(model.Classification)); err != nil {
			return fmt.Errorf("catalog: seed collection model %s: %w", model.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("catalog: commit seed: %w", err)
	}
	return nil
}

const collectionDesignColumns = `id, COALESCE(category_id, ''), display_name,
	classification, asset_bundle_id, asset_bundle_version, sort_order, tier_count`

func scanCollectionDesign(row interface{ Scan(...any) error }) (domain.CollectionDesign, error) {
	var (
		design         domain.CollectionDesign
		classification string
	)
	if err := row.Scan(&design.ID, &design.CategoryID, &design.DisplayName, &classification,
		&design.AssetBundle.ID, &design.AssetBundle.Version, &design.SortOrder, &design.TierCount); err != nil {
		return domain.CollectionDesign{}, err
	}
	design.Classification = domain.CollectionDesignClassification(classification)
	return design, nil
}

func (r *PostgresCatalogRepository) ListCollectionDesigns(ctx context.Context) ([]domain.CollectionDesign, error) {
	const query = `SELECT ` + collectionDesignColumns + ` FROM collection_designs ORDER BY sort_order, id`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("catalog: list collection designs: %w", err)
	}
	defer rows.Close()

	designs := []domain.CollectionDesign{}
	for rows.Next() {
		design, err := scanCollectionDesign(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: scan collection design: %w", err)
		}
		designs = append(designs, design)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: collection design rows: %w", err)
	}
	return designs, nil
}

func (r *PostgresCatalogRepository) FindCollectionDesign(
	ctx context.Context,
	designID string,
) (domain.CollectionDesign, bool, error) {
	const query = `SELECT ` + collectionDesignColumns + ` FROM collection_designs WHERE id = $1`
	design, err := scanCollectionDesign(database.ExecutorFrom(ctx, r.pool).QueryRow(ctx, query, designID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectionDesign{}, false, nil
	}
	if err != nil {
		return domain.CollectionDesign{}, false, fmt.Errorf("catalog: find collection design: %w", err)
	}
	return design, true, nil
}

func (r *PostgresCatalogRepository) ListCollectionCategories(ctx context.Context) ([]domain.CollectionCategory, error) {
	const query = `
		SELECT id, display_name, sort_order
		FROM collection_categories
		ORDER BY sort_order, id
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("catalog: list collection categories: %w", err)
	}
	defer rows.Close()

	categories := []domain.CollectionCategory{}
	for rows.Next() {
		var (
			category domain.CollectionCategory
			id       string
		)
		if err := rows.Scan(&id, &category.DisplayName, &category.SortOrder); err != nil {
			return nil, fmt.Errorf("catalog: scan collection category: %w", err)
		}
		category.ID = domain.CollectionCategoryID(id)
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: collection category rows: %w", err)
	}
	return categories, nil
}

func (r *PostgresCatalogRepository) CollectionCategoryExists(ctx context.Context, categoryID string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM collection_categories WHERE id = $1)`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, categoryID).Scan(&exists); err != nil {
		return false, fmt.Errorf("catalog: collection category exists: %w", err)
	}
	return exists, nil
}

func (r *PostgresCatalogRepository) ListSculptures(ctx context.Context) ([]domain.Sculpture, error) {
	const query = `
		SELECT id, display_name, asset_bundle_id, asset_bundle_version
		FROM sculptures
		ORDER BY id
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("catalog: list sculptures: %w", err)
	}
	defer rows.Close()

	var sculptures []domain.Sculpture
	for rows.Next() {
		var sculpture domain.Sculpture
		if err := rows.Scan(&sculpture.ID, &sculpture.DisplayName, &sculpture.AssetBundle.ID, &sculpture.AssetBundle.Version); err != nil {
			return nil, fmt.Errorf("catalog: scan sculpture: %w", err)
		}
		sculptures = append(sculptures, sculpture)
	}
	return sculptures, rows.Err()
}

func (r *PostgresCatalogRepository) SculptureExists(ctx context.Context, sculptureID string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM sculptures WHERE id = $1)`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, sculptureID).Scan(&exists); err != nil {
		return false, fmt.Errorf("catalog: sculpture exists: %w", err)
	}
	return exists, nil
}

const musicTrackColumns = `id, display_name, attribution, licensing, storage_key, content_type, duration_seconds`

func scanMusicTrack(row interface{ Scan(...any) error }) (domain.MusicTrack, error) {
	var (
		track     domain.MusicTrack
		id        string
		licensing string
	)
	if err := row.Scan(&id, &track.DisplayName, &track.Attribution, &licensing,
		&track.StorageKey, &track.ContentType, &track.DurationSeconds); err != nil {
		return domain.MusicTrack{}, err
	}
	track.ID = domain.MusicTrackID(id)
	track.Licensing = domain.MusicLicensing(licensing)
	return track, nil
}

func (r *PostgresCatalogRepository) ListMusicTracks(ctx context.Context) ([]domain.MusicTrack, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+musicTrackColumns+` FROM music_tracks ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: list music tracks: %w", err)
	}
	defer rows.Close()

	var tracks []domain.MusicTrack
	for rows.Next() {
		track, err := scanMusicTrack(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: scan music track: %w", err)
		}
		tracks = append(tracks, track)
	}
	return tracks, rows.Err()
}

func (r *PostgresCatalogRepository) FindMusicTrack(ctx context.Context, trackID string) (domain.MusicTrack, error) {
	track, err := scanMusicTrack(r.pool.QueryRow(ctx, `SELECT `+musicTrackColumns+` FROM music_tracks WHERE id = $1`, trackID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MusicTrack{}, domain.ErrMusicTrackNotFound
		}
		return domain.MusicTrack{}, fmt.Errorf("catalog: find music track: %w", err)
	}
	return track, nil
}

func (r *PostgresCatalogRepository) MusicTrackExists(ctx context.Context, trackID string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM music_tracks WHERE id = $1)`, trackID).Scan(&exists); err != nil {
		return false, fmt.Errorf("catalog: music track exists: %w", err)
	}
	return exists, nil
}

func (r *PostgresCatalogRepository) ListStyles(ctx context.Context) ([]domain.MuseumStyle, error) {
	const query = `
		SELECT id, display_name, asset_bundle_id, asset_bundle_version
		FROM museum_styles
		ORDER BY id
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("catalog: list styles: %w", err)
	}
	defer rows.Close()

	var styles []domain.MuseumStyle
	for rows.Next() {
		var style domain.MuseumStyle
		if err := rows.Scan(&style.ID, &style.DisplayName, &style.AssetBundle.ID, &style.AssetBundle.Version); err != nil {
			return nil, fmt.Errorf("catalog: scan style: %w", err)
		}
		styles = append(styles, style)
	}
	return styles, rows.Err()
}

func (r *PostgresCatalogRepository) ListVariants(ctx context.Context, styleID domain.StyleID) ([]domain.RoomVariant, error) {
	const query = `
		SELECT id, style_id, display_name, asset_bundle_id, asset_bundle_version
		FROM room_variants
		WHERE style_id = $1
		ORDER BY id
	`
	rows, err := r.pool.Query(ctx, query, string(styleID))
	if err != nil {
		return nil, fmt.Errorf("catalog: list variants: %w", err)
	}
	defer rows.Close()

	var variants []domain.RoomVariant
	for rows.Next() {
		var variant domain.RoomVariant
		if err := rows.Scan(&variant.ID, &variant.StyleID, &variant.DisplayName, &variant.AssetBundle.ID, &variant.AssetBundle.Version); err != nil {
			return nil, fmt.Errorf("catalog: scan variant: %w", err)
		}
		variants = append(variants, variant)
	}
	return variants, rows.Err()
}

func (r *PostgresCatalogRepository) FindVariant(ctx context.Context, variantID domain.VariantID) (domain.RoomVariant, bool, error) {
	const query = `
		SELECT id, style_id, display_name, asset_bundle_id, asset_bundle_version
		FROM room_variants
		WHERE id = $1
	`
	var variant domain.RoomVariant
	err := r.pool.QueryRow(ctx, query, string(variantID)).Scan(
		&variant.ID, &variant.StyleID, &variant.DisplayName,
		&variant.AssetBundle.ID, &variant.AssetBundle.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoomVariant{}, false, nil
	}
	if err != nil {
		return domain.RoomVariant{}, false, fmt.Errorf("catalog: find variant: %w", err)
	}
	return variant, true, nil
}

func (r *PostgresCatalogRepository) StyleExists(ctx context.Context, styleID string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM museum_styles WHERE id = $1)`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, styleID).Scan(&exists); err != nil {
		return false, fmt.Errorf("catalog: style exists: %w", err)
	}
	return exists, nil
}

func (r *PostgresCatalogRepository) VariantStyle(ctx context.Context, variantID string) (string, bool, error) {
	const query = `SELECT style_id FROM room_variants WHERE id = $1`
	var styleID string
	err := r.pool.QueryRow(ctx, query, variantID).Scan(&styleID)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("catalog: variant style: %w", err)
	}
	return styleID, true, nil
}

// MARK: - Collection Catalog: Brand and Model

const collectionModelColumns = `m.id, m.brand_id, m.category_id, m.display_name,
	b.display_name, m.search_text, m.metadata, m.asset_bundle_id,
	m.asset_bundle_version, m.classification`

func scanCollectionModel(row interface{ Scan(...any) error }) (domain.CollectionModel, error) {
	var (
		model          domain.CollectionModel
		id             string
		brandID        string
		categoryID     string
		classification string
	)
	if err := row.Scan(&id, &brandID, &categoryID, &model.DisplayName,
		&model.BrandDisplayName, &model.SearchText, &model.Metadata,
		&model.AssetBundle.ID, &model.AssetBundle.Version, &classification); err != nil {
		return domain.CollectionModel{}, err
	}
	model.ID = domain.CollectionModelID(id)
	model.BrandID = domain.CollectionBrandID(brandID)
	model.CategoryID = domain.CollectionCategoryID(categoryID)
	model.Classification = domain.CatalogContentClassification(classification)
	return model, nil
}

func (r *PostgresCatalogRepository) SearchCollectionModels(
	ctx context.Context,
	query domain.ModelSearchQuery,
) (domain.ModelSearchPage, error) {
	args := []any{string(query.CategoryID)}
	conditions := []string{"m.category_id = $1"}

	if len(query.Terms) > 0 {
		prefixed := make([]string, 0, len(query.Terms))
		for _, term := range query.Terms {
			prefixed = append(prefixed, term+":*")
		}
		args = append(args, strings.Join(prefixed, " & "))
		conditions = append(conditions,
			fmt.Sprintf("m.search_document @@ to_tsquery('simple', $%d)", len(args)))
	}

	if cursor := query.Cursor; cursor != nil {
		args = append(args, cursor.DisplayName, string(cursor.ID))
		conditions = append(conditions,
			fmt.Sprintf("(m.display_name, m.id) > ($%d, $%d)", len(args)-1, len(args)))
	}

	args = append(args, query.Limit+1)
	sql := `
		SELECT ` + collectionModelColumns + `
		FROM collection_models m
		JOIN collection_brands b ON b.id = m.brand_id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY m.display_name, m.id
		LIMIT $` + fmt.Sprint(len(args))

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return domain.ModelSearchPage{}, fmt.Errorf("catalog: search collection models: %w", err)
	}
	defer rows.Close()

	models := []domain.CollectionModel{}
	for rows.Next() {
		model, err := scanCollectionModel(rows)
		if err != nil {
			return domain.ModelSearchPage{}, fmt.Errorf("catalog: scan collection model: %w", err)
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return domain.ModelSearchPage{}, fmt.Errorf("catalog: collection model rows: %w", err)
	}

	page := domain.ModelSearchPage{Models: models}
	if len(models) > query.Limit {
		last := models[query.Limit-1]
		page.Models = models[:query.Limit]
		page.Next = &domain.ModelSearchCursor{DisplayName: last.DisplayName, ID: last.ID}
	}
	return page, nil
}

func (r *PostgresCatalogRepository) FindCollectionModels(
	ctx context.Context,
	modelIDs []string,
) ([]domain.CollectionModel, error) {
	if len(modelIDs) == 0 {
		return nil, nil
	}
	const query = `
		SELECT ` + collectionModelColumns + `
		FROM collection_models m
		JOIN collection_brands b ON b.id = m.brand_id
		WHERE m.id = ANY($1)
		ORDER BY m.id
	`
	rows, err := r.pool.Query(ctx, query, modelIDs)
	if err != nil {
		return nil, fmt.Errorf("catalog: find collection models: %w", err)
	}
	defer rows.Close()

	var models []domain.CollectionModel
	for rows.Next() {
		model, err := scanCollectionModel(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: scan collection model: %w", err)
		}
		models = append(models, model)
	}
	return models, rows.Err()
}

func (r *PostgresCatalogRepository) FindCollectionModel(
	ctx context.Context,
	modelID string,
) (domain.CollectionModel, bool, error) {
	const query = `
		SELECT ` + collectionModelColumns + `
		FROM collection_models m
		JOIN collection_brands b ON b.id = m.brand_id
		WHERE m.id = $1
	`
	model, err := scanCollectionModel(r.pool.QueryRow(ctx, query, modelID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectionModel{}, false, nil
	}
	if err != nil {
		return domain.CollectionModel{}, false, fmt.Errorf("catalog: find collection model: %w", err)
	}
	return model, true, nil
}
