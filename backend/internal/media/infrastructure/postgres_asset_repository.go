package infrastructure

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/media/application"
	"muse-backend/internal/media/domain"
	"muse-backend/internal/platform/database"
)

const postgresUniqueViolation = "23505"

type PostgresAssetRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresAssetRepository(pool *pgxpool.Pool) *PostgresAssetRepository {
	return &PostgresAssetRepository{pool: pool}
}

func (r *PostgresAssetRepository) db(ctx context.Context) database.Executor {
	return database.ExecutorFrom(ctx, r.pool)
}

const assetColumns = `id, account_id, category, storage_key, content_type, byte_size,
	pixel_width, pixel_height, checksum_sha256, state, client_upload_id,
	created_at, committed_at, released_at, discarded_at`

func (r *PostgresAssetRepository) CreatePending(ctx context.Context, asset domain.Asset) (domain.Asset, bool, error) {
	id, err := newUUIDv4()
	if err != nil {
		return domain.Asset{}, false, fmt.Errorf("postgres_asset_repository: generate id: %w", err)
	}
	asset.ID = domain.AssetID(id)
	asset.StorageKey = domain.PhotoStorageKey(asset.AccountID, asset.ID)

	const insert = `
		INSERT INTO assets (id, account_id, category, storage_key, content_type, byte_size,
			pixel_width, pixel_height, checksum_sha256, state, client_upload_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING ` + assetColumns

	row := r.db(ctx).QueryRow(ctx, insert,
		string(asset.ID), asset.AccountID, string(asset.Category), asset.StorageKey,
		asset.ContentType, asset.ByteSize, asset.PixelWidth, asset.PixelHeight,
		asset.ChecksumSHA256, string(domain.StatePending), asset.ClientUploadID, asset.CreatedAt,
	)
	created, err := scanAsset(row)
	if err == nil {
		return created, true, nil
	}
	if !isUniqueViolation(err) {
		return domain.Asset{}, false, fmt.Errorf("postgres_asset_repository: insert: %w", err)
	}

	const find = `
		SELECT ` + assetColumns + `
		FROM assets
		WHERE account_id = $1 AND client_upload_id = $2 AND state <> 'discarded'
	`
	existing, err := scanAsset(r.db(ctx).QueryRow(ctx, find, asset.AccountID, asset.ClientUploadID))
	if err != nil {
		return domain.Asset{}, false, fmt.Errorf("postgres_asset_repository: find existing after conflict: %w", err)
	}
	return existing, false, nil
}

func (r *PostgresAssetRepository) FindOwnedByIDs(ctx context.Context, accountID string, ids []domain.AssetID) ([]domain.Asset, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	const query = `
		SELECT ` + assetColumns + `
		FROM assets
		WHERE account_id = $1 AND id = ANY($2::uuid[])
	`
	rows, err := r.db(ctx).Query(ctx, query, accountID, assetIDStrings(ids))
	if err != nil {
		return nil, fmt.Errorf("postgres_asset_repository: find by ids: %w", err)
	}
	defer rows.Close()

	var assets []domain.Asset
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres_asset_repository: scan: %w", err)
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (r *PostgresAssetRepository) MarkCommitted(ctx context.Context, ids []domain.AssetID, at time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	const query = `
		UPDATE assets
		SET state = 'committed', committed_at = $2
		WHERE id = ANY($1::uuid[]) AND state = 'pending'
	`
	tag, err := r.db(ctx).Exec(ctx, query, assetIDStrings(ids), at)
	if err != nil {
		return 0, fmt.Errorf("postgres_asset_repository: mark committed: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *PostgresAssetRepository) MarkReleased(ctx context.Context, ids []domain.AssetID, at time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	const query = `
		UPDATE assets
		SET state = 'released', released_at = $2
		WHERE id = ANY($1::uuid[]) AND state = 'committed'
	`
	tag, err := r.db(ctx).Exec(ctx, query, assetIDStrings(ids), at)
	if err != nil {
		return 0, fmt.Errorf("postgres_asset_repository: mark released: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *PostgresAssetRepository) MarkDiscarded(ctx context.Context, id domain.AssetID, at time.Time) error {
	const query = `
		UPDATE assets
		SET state = 'discarded', discarded_at = $2
		WHERE id = $1 AND state IN ('pending', 'released')
	`
	tag, err := r.db(ctx).Exec(ctx, query, string(id), at)
	if err != nil {
		return fmt.Errorf("postgres_asset_repository: mark discarded: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAssetNotPending
	}
	return nil
}

func (r *PostgresAssetRepository) ListPendingOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]domain.Asset, error) {
	const query = `
		SELECT ` + assetColumns + `
		FROM assets
		WHERE state = 'pending' AND created_at < $1
		ORDER BY created_at
		LIMIT $2
	`
	rows, err := r.db(ctx).Query(ctx, query, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres_asset_repository: list pending: %w", err)
	}
	defer rows.Close()

	var assets []domain.Asset
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres_asset_repository: scan: %w", err)
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (r *PostgresAssetRepository) ListReleasedOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]domain.Asset, error) {
	const query = `
		SELECT ` + assetColumns + `
		FROM assets
		WHERE state = 'released' AND released_at < $1
		ORDER BY released_at
		LIMIT $2
	`
	rows, err := r.db(ctx).Query(ctx, query, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres_asset_repository: list released: %w", err)
	}
	defer rows.Close()

	var assets []domain.Asset
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres_asset_repository: scan: %w", err)
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

var _ application.AssetRepository = (*PostgresAssetRepository)(nil)

// MARK: - Helpers

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAsset(row rowScanner) (domain.Asset, error) {
	var (
		id, category, storageKey, contentType, checksum, state, clientUploadID string
		accountID                                                              *string
		byteSize                                                               int64
		pixelWidth, pixelHeight                                                int
		createdAt                                                              time.Time
		committedAt, releasedAt, discardedAt                                   *time.Time
	)
	if err := row.Scan(&id, &accountID, &category, &storageKey, &contentType, &byteSize,
		&pixelWidth, &pixelHeight, &checksum, &state, &clientUploadID,
		&createdAt, &committedAt, &releasedAt, &discardedAt); err != nil {
		return domain.Asset{}, err
	}
	asset := domain.Asset{
		ID:             domain.AssetID(id),
		Category:       domain.AssetCategory(category),
		StorageKey:     storageKey,
		ContentType:    contentType,
		ByteSize:       byteSize,
		PixelWidth:     pixelWidth,
		PixelHeight:    pixelHeight,
		ChecksumSHA256: checksum,
		State:          domain.AssetState(state),
		ClientUploadID: clientUploadID,
		CreatedAt:      createdAt,
		CommittedAt:    committedAt,
		ReleasedAt:     releasedAt,
		DiscardedAt:    discardedAt,
	}
	if accountID != nil {
		asset.AccountID = *accountID
	}
	return asset, nil
}

func assetIDStrings(ids []domain.AssetID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation
}

func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var _ = pgx.ErrNoRows
