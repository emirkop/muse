package main

import (
	"context"

	analyticsapp "muse-backend/internal/analytics/application"
	analyticsdomain "muse-backend/internal/analytics/domain"
)

type analyticsRecorder struct {
	analytics *analyticsapp.AnalyticsService
	newUUID   func() string
}

func (a analyticsRecorder) RecordCatalogSearch(ctx context.Context, accountID string, categoryID string, resultCount int) {
	if categoryID == "" {
		return
	}
	bucket := analyticsdomain.ResultBucket(resultCount)
	a.analytics.RecordServerSide(ctx, accountID, analyticsdomain.Draft{
		UUID:       a.newUUID(),
		Name:       analyticsdomain.EventCatalogSearchPerformed,
		CategoryID: &categoryID,
		Result:     &bucket,
	})
}

func (a analyticsRecorder) RecordItemAddRefused(ctx context.Context, accountID string, reason string) {
	a.analytics.RecordServerSide(ctx, accountID, analyticsdomain.Draft{
		UUID:   a.newUUID(),
		Name:   analyticsdomain.EventItemAddRefused,
		Reason: &reason,
	})
}
