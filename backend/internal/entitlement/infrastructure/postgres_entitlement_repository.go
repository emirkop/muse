package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/entitlement/domain"
	"muse-backend/internal/platform/database"
)

type PostgresEntitlementRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresEntitlementRepository(pool *pgxpool.Pool) *PostgresEntitlementRepository {
	return &PostgresEntitlementRepository{pool: pool}
}

func (r *PostgresEntitlementRepository) db(ctx context.Context) database.Executor {
	return database.ExecutorFrom(ctx, r.pool)
}

const transactionColumns = `original_transaction_id, transaction_id, account_id::text, product_id, bundle_id, environment,
	app_account_token::text, purchased_at, revoked_at, revocation_reason, first_verified_at, last_verified_at`

func scanTransaction(row pgx.Row) (domain.AppStoreTransaction, error) {
	var t domain.AppStoreTransaction
	if err := row.Scan(
		&t.OriginalTransactionID, &t.TransactionID, &t.AccountID, &t.ProductID, &t.BundleID, &t.Environment,
		&t.AppAccountToken, &t.PurchasedAt, &t.RevokedAt, &t.RevocationReason, &t.FirstVerifiedAt, &t.LastVerifiedAt,
	); err != nil {
		return domain.AppStoreTransaction{}, err
	}
	return t, nil
}

func (r *PostgresEntitlementRepository) Bind(ctx context.Context, t domain.AppStoreTransaction) (domain.AppStoreTransaction, error) {
	const query = `
		INSERT INTO app_store_transactions (
			original_transaction_id, transaction_id, account_id, product_id, bundle_id, environment,
			app_account_token, purchased_at, revoked_at, revocation_reason, first_verified_at, last_verified_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
		ON CONFLICT (original_transaction_id) DO UPDATE SET
			transaction_id    = EXCLUDED.transaction_id,
			revoked_at        = EXCLUDED.revoked_at,
			revocation_reason = EXCLUDED.revocation_reason,
			last_verified_at  = EXCLUDED.last_verified_at
		WHERE app_store_transactions.account_id = EXCLUDED.account_id
		RETURNING ` + transactionColumns
	bound, err := scanTransaction(r.db(ctx).QueryRow(ctx, query,
		t.OriginalTransactionID, t.TransactionID, t.AccountID, t.ProductID, t.BundleID, t.Environment,
		t.AppAccountToken, t.PurchasedAt, t.RevokedAt, t.RevocationReason, t.FirstVerifiedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AppStoreTransaction{}, domain.ErrTransactionBoundToAnotherAccount
	}
	if err != nil {
		return domain.AppStoreTransaction{}, fmt.Errorf("entitlement repository: bind: %w", err)
	}
	return bound, nil
}

func (r *PostgresEntitlementRepository) ListForAccount(ctx context.Context, accountID string) ([]domain.AppStoreTransaction, error) {
	if !isPlausibleUUID(accountID) {
		return nil, nil
	}
	rows, err := r.db(ctx).Query(ctx,
		`SELECT `+transactionColumns+` FROM app_store_transactions WHERE account_id = $1 ORDER BY purchased_at`, accountID)
	if err != nil {
		return nil, fmt.Errorf("entitlement repository: list: %w", err)
	}
	defer rows.Close()
	var out []domain.AppStoreTransaction
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, fmt.Errorf("entitlement repository: scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *PostgresEntitlementRepository) SetRevocation(ctx context.Context, originalTransactionID string, revokedAt *time.Time, reason string) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx,
		`UPDATE app_store_transactions SET revoked_at = $2, revocation_reason = $3, last_verified_at = now() WHERE original_transaction_id = $1`,
		originalTransactionID, revokedAt, reason)
	if err != nil {
		return false, fmt.Errorf("entitlement repository: set revocation: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostgresEntitlementRepository) EnsureToken(ctx context.Context, accountID string) (string, error) {
	var token string
	err := r.db(ctx).QueryRow(ctx, `
		INSERT INTO account_app_account_tokens (account_id, token) VALUES ($1, gen_random_uuid())
		ON CONFLICT (account_id) DO UPDATE SET account_id = EXCLUDED.account_id
		RETURNING token::text`, accountID).Scan(&token)
	if err != nil {
		return "", fmt.Errorf("entitlement repository: ensure token: %w", err)
	}
	return token, nil
}

func (r *PostgresEntitlementRepository) AccountForToken(ctx context.Context, token string) (string, error) {
	if !isPlausibleUUID(token) {
		return "", domain.ErrUnknownAppAccountToken
	}
	var accountID string
	err := r.db(ctx).QueryRow(ctx,
		`SELECT account_id::text FROM account_app_account_tokens WHERE token = $1`, token).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrUnknownAppAccountToken
	}
	if err != nil {
		return "", fmt.Errorf("entitlement repository: account for token: %w", err)
	}
	return accountID, nil
}

func isPlausibleUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
