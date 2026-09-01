package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/identity/domain"
)

const postgresUniqueViolation = "23505"

type PostgresAccountRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresAccountRepository(pool *pgxpool.Pool) *PostgresAccountRepository {
	return &PostgresAccountRepository{pool: pool}
}

func (r *PostgresAccountRepository) FindByLinkedIdentity(ctx context.Context, provider domain.Provider, subject string) (domain.Account, error) {
	const query = `
		SELECT a.id, a.display_name, a.avatar_id, a.created_at, a.updated_at, a.deleted_at
		FROM accounts a
		JOIN external_identities ei ON ei.account_id = a.id
		WHERE ei.provider = $1 AND ei.subject = $2
	`

	row := r.pool.QueryRow(ctx, query, string(provider), subject)
	account, err := scanAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("postgres_account_repository: find by linked identity: %w", err)
	}
	return account, nil
}

func (r *PostgresAccountRepository) CreateWithLinkedIdentity(ctx context.Context, account domain.Account, identity domain.LinkedIdentity) (domain.Account, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Account{}, fmt.Errorf("postgres_account_repository: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	const insertAccount = `
		INSERT INTO accounts (display_name)
		VALUES ($1)
		RETURNING id, display_name, avatar_id, created_at, updated_at, deleted_at
	`
	row := tx.QueryRow(ctx, insertAccount, account.DisplayName)
	created, err := scanAccount(row)
	if err != nil {
		return domain.Account{}, fmt.Errorf("postgres_account_repository: insert account: %w", err)
	}

	const insertIdentity = `
		INSERT INTO external_identities (account_id, provider, subject)
		VALUES ($1, $2, $3)
	`
	_, err = tx.Exec(ctx, insertIdentity, created.ID, string(identity.Provider), identity.Subject)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Account{}, domain.ErrLinkedIdentityAlreadyExists
		}
		return domain.Account{}, fmt.Errorf("postgres_account_repository: insert linked identity: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Account{}, fmt.Errorf("postgres_account_repository: commit: %w", err)
	}
	return created, nil
}

func (r *PostgresAccountRepository) FindByID(ctx context.Context, id domain.AccountID) (domain.Account, error) {
	const query = `
		SELECT id, display_name, avatar_id, created_at, updated_at, deleted_at
		FROM accounts
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, string(id))
	account, err := scanAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("postgres_account_repository: find by id: %w", err)
	}
	return account, nil
}

func (r *PostgresAccountRepository) UpdateDisplayName(ctx context.Context, id domain.AccountID, displayName string) error {
	const query = `
		UPDATE accounts
		SET display_name = $1, updated_at = now()
		WHERE id = $2
	`
	tag, err := r.pool.Exec(ctx, query, displayName, string(id))
	if err != nil {
		return fmt.Errorf("postgres_account_repository: update display name: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAccountNotFound
	}
	return nil
}

func (r *PostgresAccountRepository) UpdateAvatar(ctx context.Context, id domain.AccountID, avatarID domain.AvatarID) error {
	const query = `
		UPDATE accounts
		SET avatar_id = $1, updated_at = now()
		WHERE id = $2
	`
	tag, err := r.pool.Exec(ctx, query, string(avatarID), string(id))
	if err != nil {
		return fmt.Errorf("postgres_account_repository: update avatar: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAccountNotFound
	}
	return nil
}

func (r *PostgresAccountRepository) Deactivate(ctx context.Context, id domain.AccountID) error {
	const query = `
		UPDATE accounts
		SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
	`
	if _, err := r.pool.Exec(ctx, query, string(id)); err != nil {
		return fmt.Errorf("postgres_account_repository: deactivate: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanAccount(row rowScanner) (domain.Account, error) {
	var (
		id          string
		displayName string
		avatarID    string
		createdAt   time.Time
		updatedAt   time.Time
		deletedAt   *time.Time
	)
	if err := row.Scan(&id, &displayName, &avatarID, &createdAt, &updatedAt, &deletedAt); err != nil {
		return domain.Account{}, err
	}
	return domain.Account{
		ID:          domain.AccountID(id),
		DisplayName: displayName,
		AvatarID:    domain.AvatarID(avatarID),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		DeletedAt:   deletedAt,
	}, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation
}
