package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetrics_RoutePatternIsTheLabelNotThePath(t *testing.T) {
	registry := NewRegistry()
	for i := 0; i < 1000; i++ {
		registry.ObserveRequest("GET", "GET /museum/me/rooms/{roomID}", 200, time.Millisecond)
	}

	exposition := registry.Expose()
	if strings.Count(exposition, MetricRequestsTotal+"{") != 1 {
		t.Fatalf("expected exactly one request-count series, got:\n%s", exposition)
	}
	if !strings.Contains(exposition, `route="GET /museum/me/rooms/{roomID}"`) {
		t.Errorf("the pattern is not the label:\n%s", exposition)
	}
	if !strings.Contains(exposition, "} 1000") {
		t.Errorf("expected a count of 1000:\n%s", exposition)
	}
}

func TestMetrics_AnUnmatchedRequestIsOneSeriesNotOnePerPath(t *testing.T) {
	registry := NewRegistry()
	for _, path := range []string{"/wp-admin", "/.env", "/museum/me/rooms/secret-room-id"} {
		_ = path
		registry.ObserveRequest("GET", "", 404, time.Millisecond)
	}
	exposition := registry.Expose()
	if strings.Count(exposition, `route="unmatched"`) == 0 {
		t.Fatalf("expected an `unmatched` series:\n%s", exposition)
	}
	for _, probed := range []string{"wp-admin", ".env", "secret-room-id"} {
		if strings.Contains(exposition, probed) {
			t.Errorf("the exposition records the probed path %q", probed)
		}
	}
}

func TestMetrics_NoHighCardinalityLabelIsExpressible(t *testing.T) {
	registry := NewRegistry()
	registry.MarkUp()
	registry.SetDatabaseUp(true)
	registry.ObserveRequest("POST", "POST /museum/me/rooms/{roomID}/photos", 201, time.Millisecond)
	registry.ObserveEvent(EventAuthorizationRefused, CategoryAuthz, OutcomeRefused)

	exposition := registry.Expose()
	for _, forbidden := range []string{
		"account_id=", "room_id=", "museum_id=", "asset_id=", "photo=",
		"code=", "token=", "key=", "email=", "user=", "path=",
	} {
		if strings.Contains(exposition, forbidden) {
			t.Errorf("the exposition carries label %q:\n%s", forbidden, exposition)
		}
	}
	for _, expected := range []string{"method=", "route=", "status=", "event=", "category=", "outcome="} {
		if !strings.Contains(exposition, expected) {
			t.Errorf("expected label %q in:\n%s", expected, exposition)
		}
	}
}

func TestMetrics_LogAlsoCountsTheEvent(t *testing.T) {
	registry := NewRegistry()
	UseRegistry(registry)
	t.Cleanup(func() { UseRegistry(nil) })

	Log(context.Background(), discardLogger(), Event{
		Name:     EventRefreshReuseDetected,
		Category: CategoryAuthn,
		Outcome:  OutcomeRefused,
	})

	snapshot := registry.Snapshot()
	key := MetricOperationalEvents + `{event="` + EventRefreshReuseDetected +
		`",category="authn",outcome="refused"}`
	if snapshot.Counters[key] != 1 {
		t.Fatalf("the event was not counted; counters: %v", snapshot.Counters)
	}
}

func TestMetrics_ExpositionIsValidPrometheusText(t *testing.T) {
	registry := NewRegistry()
	registry.MarkUp()
	registry.ObserveRequest("GET", "GET /health", 200, 2*time.Millisecond)

	exposition := registry.Expose()
	if !strings.Contains(exposition, "# TYPE "+MetricBuildUp+" gauge") {
		t.Error("missing gauge type header")
	}
	if !strings.Contains(exposition, "# TYPE "+MetricRequestsTotal+" counter") {
		t.Error("missing counter type header")
	}
	if !strings.Contains(exposition, "# TYPE "+MetricRequestDuration+" histogram") {
		t.Error("missing histogram type header")
	}
	for _, required := range []string{`le="+Inf"`, MetricRequestDuration + "_sum", MetricRequestDuration + "_count"} {
		if !strings.Contains(exposition, required) {
			t.Errorf("missing %q:\n%s", required, exposition)
		}
	}
	if !strings.Contains(exposition, `le="0.005"} 1`) {
		t.Errorf("a 2ms observation belongs in the 5ms bucket:\n%s", exposition)
	}
	if !strings.Contains(exposition, `le="10"} 1`) {
		t.Errorf("buckets must be cumulative:\n%s", exposition)
	}
}

// MARK: - The HTTP surfaces

func TestReadiness_ReportsAndRecordsDatabaseHealth(t *testing.T) {
	registry := NewRegistry()
	handler := ReadinessHandler(registry, healthyDatabase{})

	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if registry.Snapshot().Gauges[MetricDatabaseUp] != 1 {
		t.Error("a successful probe must set muse_database_up to 1")
	}

	registry = NewRegistry()
	handler = ReadinessHandler(registry, brokenDatabase{})
	recorder = httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if registry.Snapshot().Gauges[MetricDatabaseUp] != 0 {
		t.Error("a failed probe must set muse_database_up to 0")
	}
}

func TestMetricsEndpoint_IsTokenGatedAndAbsentWhenUnconfigured(t *testing.T) {
	registry := NewRegistry()
	registry.MarkUp()

	recorder := httptest.NewRecorder()
	MetricsHandler(registry, "")(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unconfigured: status = %d, want 404", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	MetricsHandler(registry, "scrape-secret")(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", recorder.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer wrong")
	recorder = httptest.NewRecorder()
	MetricsHandler(registry, "scrape-secret")(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer scrape-secret")
	recorder = httptest.NewRecorder()
	MetricsHandler(registry, "scrape-secret")(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("correct token: status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), MetricBuildUp) {
		t.Error("the exposition is missing muse_up")
	}
}

func TestInstrument_ObservesTheMatchedPatternAndStatus(t *testing.T) {
	registry := NewRegistry()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /museum/me/rooms/{roomID}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(Instrument(registry, muxMatcher{mux}, mux))
	defer server.Close()

	response, err := http.Get(server.URL + "/museum/me/rooms/a-room-nobody-should-see")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()

	if response.Header.Get(ResponseHeaderName) == "" {
		t.Error("the request id header is missing")
	}

	exposition := registry.Expose()
	if !strings.Contains(exposition, `route="GET /museum/me/rooms/{roomID}"`) {
		t.Errorf("the matched pattern was not recorded:\n%s", exposition)
	}
	if !strings.Contains(exposition, `status="4xx"`) {
		t.Errorf("the status class was not recorded:\n%s", exposition)
	}
	if strings.Contains(exposition, "a-room-nobody-should-see") {
		t.Error("the concrete path reached a metric label")
	}
}

// MARK: - Support

type healthyDatabase struct{}

func (healthyDatabase) HealthCheck(ctx context.Context) error { return nil }

type brokenDatabase struct{}

func (brokenDatabase) HealthCheck(ctx context.Context) error {
	return errors.New("probe failed")
}

type muxMatcher struct{ mux *http.ServeMux }

func (m muxMatcher) MatchedPattern(r *http.Request) string {
	_, pattern := m.mux.Handler(r)
	return pattern
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLog_ToleratesANilContext(t *testing.T) {
	registry := NewRegistry()
	UseRegistry(registry)
	t.Cleanup(func() { UseRegistry(nil) })

	Log(nil, discardLogger(), Event{
		Name:     EventMediaReclaimFailed,
		Category: CategoryMedia,
		Outcome:  OutcomeFailed,
	})

	if len(registry.Snapshot().Counters) != 1 {
		t.Fatal("the event was not counted")
	}
}
