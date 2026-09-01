package application

import (
	"context"
	"time"

	"muse-backend/internal/analytics/domain"
)

type EventRepository interface {
	Record(ctx context.Context, accountID string, events []domain.Event) (inserted int, err error)

	Prune(ctx context.Context, before time.Time) (removed int, err error)
}
