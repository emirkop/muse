package application

import (
	"context"
	"log/slog"
	"time"

	"muse-backend/internal/analytics/domain"
)

const RawRetention = 7 * 24 * time.Hour

type AnalyticsService struct {
	repository EventRepository
	logger     *slog.Logger
	now        func() time.Time
}

func NewAnalyticsService(repository EventRepository, logger *slog.Logger, now func() time.Time) *AnalyticsService {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &AnalyticsService{repository: repository, logger: logger, now: now}
}

type Accepted struct {
	Accepted   int
	Stored     int
	Duplicates int
}

func (s *AnalyticsService) RecordFromClient(ctx context.Context, accountID string, drafts []domain.Draft) (Accepted, error) {
	events := make([]domain.Event, 0, len(drafts))
	for _, draft := range drafts {
		event, err := domain.Validate(draft, true)
		if err != nil {
			return Accepted{}, err
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return Accepted{}, nil
	}
	inserted, err := s.repository.Record(ctx, accountID, events)
	if err != nil {
		return Accepted{}, err
	}
	return Accepted{Accepted: len(events), Stored: inserted, Duplicates: len(events) - inserted}, nil
}

func (s *AnalyticsService) RecordServerSide(ctx context.Context, accountID string, draft domain.Draft) {
	event, err := domain.Validate(draft, false)
	if err != nil {
		s.logger.Warn("analytics: refused a server-side event", "reason", err.Error())
		return
	}
	if _, err := s.repository.Record(ctx, accountID, []domain.Event{event}); err != nil {
		s.logger.Warn("analytics: could not record event", "error", err)
	}
}

func (s *AnalyticsService) PruneNow(ctx context.Context) (removed int, err error) {
	return s.repository.Prune(ctx, s.now().Add(-RawRetention))
}
