package application

import "context"

type SearchRecording interface {
	RecordCatalogSearch(ctx context.Context, accountID string, categoryID string, resultCount int)
}
