package observability

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"
)

func Instrument(registry *Registry, router PatternMatching, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := NewRequestID()
		w.Header().Set(ResponseHeaderName, id)

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(recorder, r.WithContext(WithRequestID(r.Context(), id)))

		if registry != nil {
			pattern := ""
			if router != nil {
				pattern = router.MatchedPattern(r)
			}
			registry.ObserveRequest(r.Method, pattern, recorder.status, time.Since(started))
		}
	})
}

type PatternMatching interface {
	MatchedPattern(r *http.Request) string
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.written {
		s.status = code
		s.written = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	s.written = true
	return s.ResponseWriter.Write(p)
}

func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

type DatabaseProbing interface {
	HealthCheck(ctx context.Context) error
}

func ReadinessHandler(registry *Registry, database DatabaseProbing) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		ready := true
		if database == nil {
			ready = false
		} else if err := database.HealthCheck(ctx); err != nil {
			ready = false
			Log(r.Context(), nil, Event{
				Name:     EventDependencyUnavailable,
				Category: CategoryPersistence,
				Outcome:  OutcomeUnavailable,
				Reason:   ReasonNotConfigured,
				Err:      err,
			})
		}
		if registry != nil {
			registry.SetDatabaseUp(ready)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not_ready"}` + "\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}` + "\n"))
	}
}

func MetricsHandler(registry *Registry, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.NotFound(w, r)
			return
		}
		presented := r.Header.Get("Authorization")
		expected := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"authentication required"}` + "\n"))
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(registry.Expose()))
	}
}
