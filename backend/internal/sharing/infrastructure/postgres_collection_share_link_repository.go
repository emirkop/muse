package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/sharing/domain"
)

type PostgresCollectionShareLinkRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCollectionShareLinkRepository(pool *pgxpool.Pool) *PostgresCollectionShareLinkRepository {
	return &PostgresCollectionShareLinkRepository{pool: pool}
}

const collectionLinkColumns = `id, collection_room_id, code, status, created_at, revoked_at`

func scanCollectionLink(row rowScanner) (domain.CollectionShareLink, error) {
	var (
		link    domain.CollectionShareLink
		code    string
		status  string
		revoked *time.Time
	)
	if err := row.Scan(&link.ID, &link.CollectionRoomID, &code, &status, &link.CreatedAt, &revoked); err != nil {
		return domain.CollectionShareLink{}, err
	}
	link.Code = domain.Code(code)
	link.Status = domain.Status(status)
	link.RevokedAt = revoked
	return link, nil
}

func (r *PostgresCollectionShareLinkRepository) FindActiveByRoom(ctx context.Context, collectionRoomID string) (domain.CollectionShareLink, error) {
	link, err := scanCollectionLink(r.pool.QueryRow(ctx,
		`SELECT `+collectionLinkColumns+` FROM collection_share_links WHERE collection_room_id = $1 AND status = 'active'`,
		collectionRoomID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CollectionShareLink{}, domain.ErrNoActiveCollectionLink
		}
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: find active collection link: %w", err)
	}
	return link, nil
}

func (r *PostgresCollectionShareLinkRepository) FindByCode(ctx context.Context, code domain.Code) (domain.CollectionShareLink, error) {
	link, err := scanCollectionLink(r.pool.QueryRow(ctx,
		`SELECT `+collectionLinkColumns+` FROM collection_share_links WHERE code = $1`, string(code)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CollectionShareLink{}, domain.ErrLinkNotAvailable
		}
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: find collection link by code: %w", err)
	}
	return link, nil
}

func (r *PostgresCollectionShareLinkRepository) EnsureActive(ctx context.Context, collectionRoomID string, code domain.Code, now time.Time) (link domain.CollectionShareLink, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = lockCollectionRoom(ctx, tx, collectionRoomID); err != nil {
		return domain.CollectionShareLink{}, err
	}
	existing, err := scanCollectionLink(tx.QueryRow(ctx,
		`SELECT `+collectionLinkColumns+` FROM collection_share_links WHERE collection_room_id = $1 AND status = 'active'`,
		collectionRoomID))
	switch {
	case err == nil:
		if err = tx.Commit(ctx); err != nil {
			return domain.CollectionShareLink{}, fmt.Errorf("sharing: commit: %w", err)
		}
		return existing, nil
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: find active collection link: %w", err)
	}

	link, err = insertActiveCollectionLink(ctx, tx, collectionRoomID, code, now)
	if err != nil {
		return domain.CollectionShareLink{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: commit: %w", err)
	}
	return link, nil
}

func (r *PostgresCollectionShareLinkRepository) Rotate(ctx context.Context, collectionRoomID string, code domain.Code, now time.Time) (link domain.CollectionShareLink, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = lockCollectionRoom(ctx, tx, collectionRoomID); err != nil {
		return domain.CollectionShareLink{}, err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE collection_share_links SET status = 'revoked', revoked_at = $2 WHERE collection_room_id = $1 AND status = 'active'`,
		collectionRoomID, now); err != nil {
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: revoke active collection link: %w", err)
	}
	link, err = insertActiveCollectionLink(ctx, tx, collectionRoomID, code, now)
	if err != nil {
		return domain.CollectionShareLink{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: commit: %w", err)
	}
	return link, nil
}

func (r *PostgresCollectionShareLinkRepository) Revoke(ctx context.Context, collectionRoomID string, now time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE collection_share_links SET status = 'revoked', revoked_at = $2 WHERE collection_room_id = $1 AND status = 'active'`,
		collectionRoomID, now)
	if err != nil {
		return false, fmt.Errorf("sharing: revoke collection link: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func lockCollectionRoom(ctx context.Context, tx pgx.Tx, collectionRoomID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, collectionRoomID); err != nil {
		return fmt.Errorf("sharing: lock collection room: %w", err)
	}
	return nil
}

func insertActiveCollectionLink(ctx context.Context, tx pgx.Tx, collectionRoomID string, code domain.Code, now time.Time) (domain.CollectionShareLink, error) {
	link, err := scanCollectionLink(tx.QueryRow(ctx,
		`INSERT INTO collection_share_links (collection_room_id, code, status, created_at)
		 VALUES ($1, $2, 'active', $3)
		 RETURNING `+collectionLinkColumns, collectionRoomID, string(code), now))
	if err != nil {
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: insert collection link: %w", err)
	}
	return link, nil
}
