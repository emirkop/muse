package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/catalog/application"
	"muse-backend/internal/catalog/domain"
	"muse-backend/internal/platform/database"
)

type PostgresBundleRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresBundleRepository(pool *pgxpool.Pool) *PostgresBundleRepository {
	return &PostgresBundleRepository{pool: pool}
}

func (r *PostgresBundleRepository) db(ctx context.Context) database.Executor {
	return database.ExecutorFrom(ctx, r.pool)
}

var _ application.BundleRepository = (*PostgresBundleRepository)(nil)

func (r *PostgresBundleRepository) ResolveForApp(ctx context.Context, bundleID string, appAssetVersion int) (domain.AssetBundle, error) {
	const query = `
		SELECT bundle_id, version, kind, format, min_app_version
		FROM asset_bundles
		WHERE bundle_id = $1 AND min_app_version <= $2
		ORDER BY version DESC
		LIMIT 1
	`
	bundle, err := scanBundle(r.db(ctx).QueryRow(ctx, query, bundleID, appAssetVersion))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssetBundle{}, domain.ErrBundleNotFound
		}
		return domain.AssetBundle{}, fmt.Errorf("catalog: resolve bundle: %w", err)
	}
	return r.loadParts(ctx, bundle)
}

func (r *PostgresBundleRepository) FindVersion(ctx context.Context, bundleID string, version int) (domain.AssetBundle, error) {
	const query = `
		SELECT bundle_id, version, kind, format, min_app_version
		FROM asset_bundles
		WHERE bundle_id = $1 AND version = $2
	`
	bundle, err := scanBundle(r.db(ctx).QueryRow(ctx, query, bundleID, version))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssetBundle{}, domain.ErrBundleNotFound
		}
		return domain.AssetBundle{}, fmt.Errorf("catalog: find bundle version: %w", err)
	}
	return r.loadParts(ctx, bundle)
}

func (r *PostgresBundleRepository) loadParts(ctx context.Context, bundle domain.AssetBundle) (domain.AssetBundle, error) {
	files, err := r.files(ctx, bundle.BundleID, bundle.Version)
	if err != nil {
		return domain.AssetBundle{}, err
	}
	dependencies, err := r.dependencies(ctx, bundle.BundleID, bundle.Version)
	if err != nil {
		return domain.AssetBundle{}, err
	}
	bundle.Files = files
	bundle.Dependencies = dependencies
	capacities, err := r.tierCapacities(ctx, bundle.BundleID, bundle.Version)
	if err != nil {
		return domain.AssetBundle{}, err
	}
	bundle.TierCapacities = capacities
	return bundle, nil
}

func (r *PostgresBundleRepository) tierCapacities(ctx context.Context, bundleID string, version int) (domain.TierCapacities, error) {
	const query = `
		SELECT tier, cumulative_capacity
		FROM asset_bundle_tier_capacities
		WHERE bundle_id = $1 AND version = $2
		ORDER BY tier
	`
	rows, err := r.db(ctx).Query(ctx, query, bundleID, version)
	if err != nil {
		return nil, fmt.Errorf("catalog: bundle tier capacities: %w", err)
	}
	defer rows.Close()

	var capacities domain.TierCapacities
	for rows.Next() {
		var capacity domain.TierCapacity
		if err := rows.Scan(&capacity.Tier, &capacity.Cumulative); err != nil {
			return nil, fmt.Errorf("catalog: scan bundle tier capacity: %w", err)
		}
		capacities = append(capacities, capacity)
	}
	return capacities, rows.Err()
}

func (r *PostgresBundleRepository) RegisterTierCapacities(
	ctx context.Context,
	bundleID string,
	version int,
	capacities domain.TierCapacities,
) error {
	if err := capacities.Validate(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin register tier capacities: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var existing int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM asset_bundle_tier_capacities WHERE bundle_id = $1 AND version = $2`,
		bundleID, version).Scan(&existing); err != nil {
		return fmt.Errorf("catalog: count tier capacities: %w", err)
	}
	if existing > 0 {
		return fmt.Errorf("%w: %s v%d already has a tier capacity projection",
			domain.ErrBundleVersionImmutable, bundleID, version)
	}
	if err := insertTierCapacities(ctx, tx, bundleID, version, capacities); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("catalog: commit register tier capacities: %w", err)
	}
	return nil
}

func insertTierCapacities(ctx context.Context, tx pgx.Tx, bundleID string, version int, capacities domain.TierCapacities) error {
	const insert = `
		INSERT INTO asset_bundle_tier_capacities (bundle_id, version, tier, cumulative_capacity)
		VALUES ($1, $2, $3, $4)
	`
	for _, capacity := range capacities {
		if _, err := tx.Exec(ctx, insert, bundleID, version, capacity.Tier, capacity.Cumulative); err != nil {
			return fmt.Errorf("catalog: insert tier capacity %s v%d tier %d: %w", bundleID, version, capacity.Tier, err)
		}
	}
	return nil
}

func (r *PostgresBundleRepository) DesignTierCountsNaming(ctx context.Context, bundleID string) ([]int, error) {
	rows, err := r.db(ctx).Query(ctx,
		`SELECT tier_count FROM collection_designs WHERE asset_bundle_id = $1 ORDER BY id`, bundleID)
	if err != nil {
		return nil, fmt.Errorf("catalog: design tier counts: %w", err)
	}
	defer rows.Close()

	var counts []int
	for rows.Next() {
		var count int
		if err := rows.Scan(&count); err != nil {
			return nil, fmt.Errorf("catalog: scan design tier count: %w", err)
		}
		counts = append(counts, count)
	}
	return counts, rows.Err()
}

func (r *PostgresBundleRepository) files(ctx context.Context, bundleID string, version int) ([]domain.BundleFile, error) {
	const query = `
		SELECT asset_id, role, storage_key, content_type, byte_size, checksum_sha256
		FROM asset_bundle_files
		WHERE bundle_id = $1 AND version = $2
		ORDER BY asset_id
	`
	rows, err := r.db(ctx).Query(ctx, query, bundleID, version)
	if err != nil {
		return nil, fmt.Errorf("catalog: bundle files: %w", err)
	}
	defer rows.Close()

	var files []domain.BundleFile
	for rows.Next() {
		var (
			file domain.BundleFile
			role string
		)
		if err := rows.Scan(&file.AssetID, &role, &file.StorageKey, &file.ContentType,
			&file.ByteSize, &file.ChecksumSHA256); err != nil {
			return nil, fmt.Errorf("catalog: scan bundle file: %w", err)
		}
		file.Role = domain.AssetRole(role)
		files = append(files, file)
	}
	return files, rows.Err()
}

func (r *PostgresBundleRepository) dependencies(ctx context.Context, bundleID string, version int) ([]domain.BundleDependency, error) {
	const query = `
		SELECT depends_on_bundle_id, depends_on_version
		FROM asset_bundle_dependencies
		WHERE bundle_id = $1 AND version = $2
		ORDER BY depends_on_bundle_id
	`
	rows, err := r.db(ctx).Query(ctx, query, bundleID, version)
	if err != nil {
		return nil, fmt.Errorf("catalog: bundle dependencies: %w", err)
	}
	defer rows.Close()

	var dependencies []domain.BundleDependency
	for rows.Next() {
		var dependency domain.BundleDependency
		if err := rows.Scan(&dependency.BundleID, &dependency.Version); err != nil {
			return nil, fmt.Errorf("catalog: scan bundle dependency: %w", err)
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, rows.Err()
}

func (r *PostgresBundleRepository) Register(ctx context.Context, bundle domain.AssetBundle) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin register bundle: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	const insertBundle = `
		INSERT INTO asset_bundles (bundle_id, version, kind, format, min_app_version)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := tx.Exec(ctx, insertBundle, bundle.BundleID, bundle.Version,
		string(bundle.Kind), bundle.Format, bundle.MinAppVersion); err != nil {
		return fmt.Errorf("catalog: insert bundle %s v%d: %w", bundle.BundleID, bundle.Version, err)
	}

	const insertFile = `
		INSERT INTO asset_bundle_files
			(bundle_id, version, asset_id, role, storage_key, content_type, byte_size, checksum_sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	for _, file := range bundle.Files {
		if _, err := tx.Exec(ctx, insertFile, bundle.BundleID, bundle.Version, file.AssetID,
			string(file.Role), file.StorageKey, file.ContentType, file.ByteSize, file.ChecksumSHA256); err != nil {
			return fmt.Errorf("catalog: insert bundle file %s: %w", file.AssetID, err)
		}
	}

	const insertDependency = `
		INSERT INTO asset_bundle_dependencies
			(bundle_id, version, depends_on_bundle_id, depends_on_version)
		VALUES ($1, $2, $3, $4)
	`
	for _, dependency := range bundle.Dependencies {
		if _, err := tx.Exec(ctx, insertDependency, bundle.BundleID, bundle.Version,
			dependency.BundleID, dependency.Version); err != nil {
			return fmt.Errorf("catalog: insert bundle dependency %s: %w", dependency.BundleID, err)
		}
	}

	if err := insertTierCapacities(ctx, tx, bundle.BundleID, bundle.Version, bundle.TierCapacities); err != nil {
		return err
	}

	for _, table := range []string{"museum_styles", "room_variants", "sculptures"} {
		query := fmt.Sprintf(
			`UPDATE %s SET asset_bundle_version = $1 WHERE asset_bundle_id = $2`, table)
		if _, err := tx.Exec(ctx, query, bundle.Version, bundle.BundleID); err != nil {
			return fmt.Errorf("catalog: point %s at %s v%d: %w", table, bundle.BundleID, bundle.Version, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("catalog: commit register bundle: %w", err)
	}
	return nil
}

func scanBundle(row interface{ Scan(...any) error }) (domain.AssetBundle, error) {
	var (
		bundle domain.AssetBundle
		kind   string
	)
	if err := row.Scan(&bundle.BundleID, &bundle.Version, &kind, &bundle.Format, &bundle.MinAppVersion); err != nil {
		return domain.AssetBundle{}, err
	}
	bundle.Kind = domain.BundleKind(kind)
	return bundle, nil
}
