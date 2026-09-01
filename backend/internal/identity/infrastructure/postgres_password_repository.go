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

type PostgresPasswordRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresPasswordRepository(pool *pgxpool.Pool) *PostgresPasswordRepository {
	return &PostgresPasswordRepository{pool: pool}
}

func (r *PostgresPasswordRepository) CreateAccountWithCredential(
	ctx context.Context,
	account domain.Account,
	credential domain.PasswordCredential,
) (domain.Account, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Account{}, fmt.Errorf("password_repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		id        string
		createdAt time.Time
		updatedAt time.Time
	)
	err = tx.QueryRow(ctx,
		`INSERT INTO accounts (display_name) VALUES ('')
		 RETURNING id, created_at, updated_at`,
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		return domain.Account{}, fmt.Errorf("password_repository: insert account: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO password_credentials (account_id, email, password_hash)
		 VALUES ($1, $2, $3)`,
		id, string(credential.Email), credential.Hash,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation {
			return domain.Account{}, domain.ErrEmailAlreadyRegistered
		}
		return domain.Account{}, fmt.Errorf("password_repository: insert credential: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Account{}, fmt.Errorf("password_repository: commit: %w", err)
	}

	return domain.Account{
		ID:        domain.AccountID(id),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (r *PostgresPasswordRepository) FindByEmail(ctx context.Context, email domain.EmailAddress) (domain.PasswordCredential, error) {
	return r.findOne(ctx,
		`SELECT account_id, email, password_hash, created_at, updated_at
		   FROM password_credentials WHERE email = $1`,
		string(email),
	)
}

func (r *PostgresPasswordRepository) FindByAccountID(ctx context.Context, accountID domain.AccountID) (domain.PasswordCredential, error) {
	return r.findOne(ctx,
		`SELECT account_id, email, password_hash, created_at, updated_at
		   FROM password_credentials WHERE account_id = $1`,
		string(accountID),
	)
}

func (r *PostgresPasswordRepository) findOne(ctx context.Context, query string, arg any) (domain.PasswordCredential, error) {
	var (
		accountID string
		email     string
		hash      string
		createdAt time.Time
		updatedAt time.Time
	)
	err := r.pool.QueryRow(ctx, query, arg).Scan(&accountID, &email, &hash, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PasswordCredential{}, domain.ErrCredentialNotFound
	}
	if err != nil {
		return domain.PasswordCredential{}, fmt.Errorf("password_repository: query credential: %w", err)
	}
	return domain.PasswordCredential{
		AccountID: domain.AccountID(accountID),
		Email:     domain.EmailAddress(email),
		Hash:      hash,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (r *PostgresPasswordRepository) UpdateHash(ctx context.Context, accountID domain.AccountID, encodedHash string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE password_credentials
		    SET password_hash = $2, updated_at = now()
		  WHERE account_id = $1`,
		string(accountID), encodedHash,
	)
	if err != nil {
		return fmt.Errorf("password_repository: update hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCredentialNotFound
	}
	return nil
}

type PostgresPendingSignupRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresPendingSignupRepository(pool *pgxpool.Pool) *PostgresPendingSignupRepository {
	return &PostgresPendingSignupRepository{pool: pool}
}

func (r *PostgresPendingSignupRepository) Upsert(ctx context.Context, signup domain.PendingSignup) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO pending_signups (id, email, password_hash, token_digest, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (email) DO UPDATE
		    SET password_hash = EXCLUDED.password_hash,
		        token_digest  = EXCLUDED.token_digest,
		        expires_at    = EXCLUDED.expires_at,
		        created_at    = EXCLUDED.created_at,
		        consumed_at   = NULL`,
		signup.ID, string(signup.Email), signup.PasswordHash,
		signup.TokenDigest, signup.ExpiresAt, signup.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("pending_signup_repository: upsert: %w", err)
	}
	return nil
}

func (r *PostgresPendingSignupRepository) FindByTokenDigest(ctx context.Context, digest string) (domain.PendingSignup, error) {
	return r.findOne(ctx,
		`SELECT id, email, password_hash, token_digest, expires_at, created_at, consumed_at
		   FROM pending_signups WHERE token_digest = $1`,
		digest,
	)
}

func (r *PostgresPendingSignupRepository) FindByEmail(ctx context.Context, email domain.EmailAddress) (domain.PendingSignup, error) {
	return r.findOne(ctx,
		`SELECT id, email, password_hash, token_digest, expires_at, created_at, consumed_at
		   FROM pending_signups WHERE email = $1`,
		string(email),
	)
}

func (r *PostgresPendingSignupRepository) findOne(ctx context.Context, query string, arg any) (domain.PendingSignup, error) {
	var (
		signup     domain.PendingSignup
		email      string
		consumedAt *time.Time
	)
	err := r.pool.QueryRow(ctx, query, arg).Scan(
		&signup.ID, &email, &signup.PasswordHash, &signup.TokenDigest,
		&signup.ExpiresAt, &signup.CreatedAt, &consumedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PendingSignup{}, domain.ErrVerificationTokenInvalid
	}
	if err != nil {
		return domain.PendingSignup{}, fmt.Errorf("pending_signup_repository: query: %w", err)
	}
	signup.Email = domain.EmailAddress(email)
	signup.ConsumedAt = consumedAt
	return signup, nil
}

func (r *PostgresPendingSignupRepository) Consume(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE pending_signups SET consumed_at = now()
		  WHERE id = $1 AND consumed_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("pending_signup_repository: consume: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrVerificationTokenInvalid
	}
	return nil
}

type PostgresPasswordResetRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresPasswordResetRepository(pool *pgxpool.Pool) *PostgresPasswordResetRepository {
	return &PostgresPasswordResetRepository{pool: pool}
}

func (r *PostgresPasswordResetRepository) Create(ctx context.Context, reset domain.PasswordReset) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO password_resets (id, account_id, token_digest, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		reset.ID, string(reset.AccountID), reset.TokenDigest, reset.ExpiresAt, reset.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("password_reset_repository: insert: %w", err)
	}
	return nil
}

func (r *PostgresPasswordResetRepository) FindByTokenDigest(ctx context.Context, digest string) (domain.PasswordReset, error) {
	var (
		reset      domain.PasswordReset
		accountID  string
		consumedAt *time.Time
	)
	err := r.pool.QueryRow(ctx,
		`SELECT id, account_id, token_digest, expires_at, created_at, consumed_at
		   FROM password_resets WHERE token_digest = $1`,
		digest,
	).Scan(&reset.ID, &accountID, &reset.TokenDigest, &reset.ExpiresAt, &reset.CreatedAt, &consumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PasswordReset{}, domain.ErrResetTokenInvalid
	}
	if err != nil {
		return domain.PasswordReset{}, fmt.Errorf("password_reset_repository: query: %w", err)
	}
	reset.AccountID = domain.AccountID(accountID)
	reset.ConsumedAt = consumedAt
	return reset, nil
}

func (r *PostgresPasswordResetRepository) Consume(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE password_resets SET consumed_at = now()
		  WHERE id = $1 AND consumed_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("password_reset_repository: consume: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrResetTokenInvalid
	}
	return nil
}

func (r *PostgresPasswordResetRepository) InvalidateAllForAccount(ctx context.Context, accountID domain.AccountID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE password_resets SET consumed_at = now()
		  WHERE account_id = $1 AND consumed_at IS NULL`,
		string(accountID),
	)
	if err != nil {
		return fmt.Errorf("password_reset_repository: invalidate all: %w", err)
	}
	return nil
}
