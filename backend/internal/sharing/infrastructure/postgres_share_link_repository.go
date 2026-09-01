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

type PostgresShareLinkRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresShareLinkRepository(pool *pgxpool.Pool) *PostgresShareLinkRepository {
	return &PostgresShareLinkRepository{pool: pool}
}

const linkColumns = `id, museum_id, code, status, created_at, revoked_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLink(row rowScanner) (domain.ShareLink, error) {
	var (
		link    domain.ShareLink
		code    string
		status  string
		revoked *time.Time
	)
	if err := row.Scan(&link.ID, &link.MuseumID, &code, &status, &link.CreatedAt, &revoked); err != nil {
		return domain.ShareLink{}, err
	}
	link.Code = domain.Code(code)
	link.Status = domain.Status(status)
	link.RevokedAt = revoked
	return link, nil
}

func (r *PostgresShareLinkRepository) FindActiveByMuseum(ctx context.Context, museumID string) (domain.ShareLink, error) {
	link, err := scanLink(r.pool.QueryRow(ctx,
		`SELECT `+linkColumns+` FROM share_links WHERE museum_id = $1 AND status = 'active'`, museumID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ShareLink{}, domain.ErrNoActiveLink
		}
		return domain.ShareLink{}, fmt.Errorf("sharing: find active link: %w", err)
	}
	return link, nil
}

func (r *PostgresShareLinkRepository) FindByCode(ctx context.Context, code domain.Code) (domain.ShareLink, error) {
	link, err := scanLink(r.pool.QueryRow(ctx,
		`SELECT `+linkColumns+` FROM share_links WHERE code = $1`, string(code)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ShareLink{}, domain.ErrLinkNotAvailable
		}
		return domain.ShareLink{}, fmt.Errorf("sharing: find link by code: %w", err)
	}
	return link, nil
}

func (r *PostgresShareLinkRepository) EnsureActive(ctx context.Context, museumID string, code domain.Code, now time.Time) (link domain.ShareLink, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ShareLink{}, fmt.Errorf("sharing: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = lockMuseum(ctx, tx, museumID); err != nil {
		return domain.ShareLink{}, err
	}
	existing, err := scanLink(tx.QueryRow(ctx,
		`SELECT `+linkColumns+` FROM share_links WHERE museum_id = $1 AND status = 'active'`, museumID))
	switch {
	case err == nil:
		if err = tx.Commit(ctx); err != nil {
			return domain.ShareLink{}, fmt.Errorf("sharing: commit: %w", err)
		}
		return existing, nil
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return domain.ShareLink{}, fmt.Errorf("sharing: find active link: %w", err)
	}

	link, err = insertActive(ctx, tx, museumID, code, now)
	if err != nil {
		return domain.ShareLink{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ShareLink{}, fmt.Errorf("sharing: commit: %w", err)
	}
	return link, nil
}

func (r *PostgresShareLinkRepository) Rotate(ctx context.Context, museumID string, code domain.Code, now time.Time) (link domain.ShareLink, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ShareLink{}, fmt.Errorf("sharing: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = lockMuseum(ctx, tx, museumID); err != nil {
		return domain.ShareLink{}, err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE share_links SET status = 'revoked', revoked_at = $2 WHERE museum_id = $1 AND status = 'active'`,
		museumID, now); err != nil {
		return domain.ShareLink{}, fmt.Errorf("sharing: revoke active link: %w", err)
	}
	link, err = insertActive(ctx, tx, museumID, code, now)
	if err != nil {
		return domain.ShareLink{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ShareLink{}, fmt.Errorf("sharing: commit: %w", err)
	}
	return link, nil
}

func lockMuseum(ctx context.Context, tx pgx.Tx, museumID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, museumID); err != nil {
		return fmt.Errorf("sharing: lock museum: %w", err)
	}
	return nil
}

func insertActive(ctx context.Context, tx pgx.Tx, museumID string, code domain.Code, now time.Time) (domain.ShareLink, error) {
	link, err := scanLink(tx.QueryRow(ctx,
		`INSERT INTO share_links (museum_id, code, status, created_at)
		 VALUES ($1, $2, 'active', $3)
		 RETURNING `+linkColumns, museumID, string(code), now))
	if err != nil {
		return domain.ShareLink{}, fmt.Errorf("sharing: insert link: %w", err)
	}
	return link, nil
}
