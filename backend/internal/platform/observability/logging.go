package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"sync"
)

const (
	FieldEvent     = "event"
	FieldCategory  = "category"
	FieldOutcome   = "outcome"
	FieldRequestID = "request_id"
	FieldAccountID = "account_id"
	FieldReason    = "reason"
	FieldError     = "error"
)

type Category string

const (
	CategoryAuthn         Category = "authn"
	CategoryAuthz         Category = "authz"
	CategoryEntitlement   Category = "entitlement"
	CategorySharing       Category = "sharing"
	CategoryAssetDelivery Category = "asset_delivery"
	CategoryMedia         Category = "media"
	CategoryPersistence   Category = "persistence"
	CategoryConfig        Category = "config"
	CategoryEmail         Category = "email"
	CategoryAnalytics     Category = "analytics"
)

type Outcome string

const (
	OutcomeRefused     Outcome = "refused"
	OutcomeFailed      Outcome = "failed"
	OutcomeUnavailable Outcome = "unavailable"
)

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

const ResponseHeaderName = "X-Muse-Request-Id"

func WithRequestIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := NewRequestID()
		w.Header().Set(ResponseHeaderName, id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

func Logger(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	if id := RequestID(ctx); id != "" {
		return base.With(FieldRequestID, id)
	}
	return base
}

type Event struct {
	Name      string
	Category  Category
	Outcome   Outcome
	Reason    string
	AccountID string
	Err       error
}

var (
	metricsMu       sync.RWMutex
	metricsRegistry *Registry
)

func UseRegistry(registry *Registry) {
	metricsMu.Lock()
	metricsRegistry = registry
	metricsMu.Unlock()
}

func currentRegistry() *Registry {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	return metricsRegistry
}

func Log(ctx context.Context, base *slog.Logger, event Event) {
	LogWith(ctx, base, event)
}

func LogWith(ctx context.Context, base *slog.Logger, event Event, extra ...any) {
	if registry := currentRegistry(); registry != nil {
		registry.ObserveEvent(event.Name, event.Category, event.Outcome)
	}
	logger := Logger(ctx, base)

	attrs := []any{
		FieldEvent, event.Name,
		FieldCategory, string(event.Category),
		FieldOutcome, string(event.Outcome),
	}
	if event.Reason != "" {
		attrs = append(attrs, FieldReason, event.Reason)
	}
	if event.AccountID != "" {
		attrs = append(attrs, FieldAccountID, event.AccountID)
	}
	if event.Err != nil {
		attrs = append(attrs, FieldError, event.Err.Error())
	}
	attrs = append(attrs, extra...)

	switch event.Outcome {
	case OutcomeFailed:
		logger.Error(event.Name, attrs...)
	case OutcomeUnavailable:
		logger.Warn(event.Name, attrs...)
	default:
		logger.Info(event.Name, attrs...)
	}
}
