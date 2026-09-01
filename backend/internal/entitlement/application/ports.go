package application

import (
	"context"
	"time"

	"muse-backend/internal/entitlement/domain"
)

type TransactionRepository interface {
	Bind(ctx context.Context, tx domain.AppStoreTransaction) (domain.AppStoreTransaction, error)
	ListForAccount(ctx context.Context, accountID string) ([]domain.AppStoreTransaction, error)
	SetRevocation(ctx context.Context, originalTransactionID string, revokedAt *time.Time, reason string) (bool, error)
}

type AppAccountTokenRepository interface {
	EnsureToken(ctx context.Context, accountID string) (string, error)
	AccountForToken(ctx context.Context, token string) (string, error)
}

type CollectionItemCounting interface {
	CountItemsForAccount(ctx context.Context, accountID string) (int, error)
}

type SignedTransactionVerifying interface {
	VerifyTransaction(ctx context.Context, signedTransaction string) (domain.VerifiedTransaction, error)
	VerifyNotification(ctx context.Context, signedPayload string) (domain.Notification, error)
}

type Clock func() time.Time
