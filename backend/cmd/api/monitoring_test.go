package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/platform/observability"
)

func TestReadinessReportsDatabaseHealthAndSetsTheGauge(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodGet, "/health/ready", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"ready"`) {
		t.Errorf("body = %s", body)
	}
	if snapshot := s.metrics.Snapshot(); snapshot.Gauges[observability.MetricDatabaseUp] != 1 {
		t.Errorf("muse_database_up = %v, want 1", snapshot.Gauges[observability.MetricDatabaseUp])
	}

	live, _ := s.do(http.MethodGet, "/health", nil, "")
	if live.StatusCode != http.StatusOK {
		t.Fatalf("liveness status = %d", live.StatusCode)
	}
}

func TestRealTrafficProducesTheMetricsTheRulesRead(t *testing.T) {
	s := newStack(t)
	room := s.createRoom()

	if resp, _ := s.do(http.MethodGet, "/health/ready", nil, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("readiness failed")
	}

	if resp, _ := s.do(http.MethodGet, "/museum/me", nil, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("museum read failed")
	}
	if resp, _ := s.do(http.MethodGet, "/museum/me/rooms/"+bogusUUID, nil, s.token); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected a 404 for a nonexistent Room")
	}
	if resp, _ := s.do(http.MethodGet, "/catalog/bundles/no-such-bundle/manifest", nil, s.token); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected a 404 for an unpublished bundle")
	}
	_ = room

	exposition := s.metricsExposition(t)

	for _, required := range []string{
		observability.MetricBuildUp,
		observability.MetricDatabaseUp,
		observability.MetricRequestsTotal,
		observability.MetricRequestDuration,
		observability.MetricOperationalEvents,
	} {
		if !strings.Contains(exposition, required) {
			t.Errorf("the exposition is missing %s:\n%s", required, exposition)
		}
	}
	if !strings.Contains(exposition, `route="GET /museum/me/rooms/{roomID}"`) {
		t.Errorf("no series for the Room route pattern:\n%s", exposition)
	}
	if strings.Contains(exposition, bogusUUID) {
		t.Error("the exposition contains a request path's id")
	}
	if !strings.Contains(exposition, `event="`+observability.EventAuthorizationRefused+`"`) {
		t.Errorf("the authz refusal was not counted:\n%s", exposition)
	}
	if !strings.Contains(exposition, `category="asset_delivery"`) {
		t.Errorf("the asset-delivery event was not counted:\n%s", exposition)
	}
}

func TestNoUserScopedValueReachesAMetric(t *testing.T) {
	s := newStack(t)
	room := s.createRoom()
	photo := s.uploaded(newPhoto(t, 640, 480, "metrics-cardinality"))
	if resp, _, _ := s.assign(room, []string{photo.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign failed")
	}
	linkResp, linkBody := s.do(http.MethodPost, "/museum/me/share-link", nil, s.token)
	if linkResp.StatusCode != http.StatusOK && linkResp.StatusCode != http.StatusCreated {
		t.Fatalf("share link: %d %s", linkResp.StatusCode, linkBody)
	}
	s.do(http.MethodGet, "/museum/me/rooms/"+room, nil, s.token)
	s.do(http.MethodDelete, "/museum/me/rooms/"+room+"/photos/"+photo.asset, nil, s.token)
	s.do(http.MethodGet, "/share-links/aaaaaaaaaaaaaaaaaaaaaa/museum", nil, s.token)

	exposition := s.metricsExposition(t)
	for what, value := range map[string]string{
		"a Room id":          room,
		"a photograph id":    photo.asset,
		"the caller's token": s.token,
		"the account id":     s.accountID,
	} {
		if value == "" {
			t.Fatalf("no value for %s", what)
		}
		if strings.Contains(exposition, value) {
			t.Errorf("the exposition contains %s — a metric label is retained forever and multiplies series", what)
		}
	}
	series := strings.Count(exposition, observability.MetricRequestsTotal+"{")
	if series > 40 {
		t.Errorf("%d request series for a handful of routes — cardinality is not bounded", series)
	}
}

func TestMetricsEndpointIsNotOpen(t *testing.T) {
	s := newStack(t)

	resp, _ := s.do(http.MethodGet, "/metrics", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unconfigured /metrics answered %d, want 404", resp.StatusCode)
	}
	resp, _ = s.do(http.MethodGet, "/metrics", nil, s.token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("an account token answered %d; a scraper authenticates with the scrape token, not a session", resp.StatusCode)
	}
}

func TestASimulatedFailureInRealTrafficFiresTheExpectedAlert(t *testing.T) {
	s := newStack(t)
	before := s.metrics.Snapshot()

	for i := 0; i < 60; i++ {
		resp, _ := s.do(http.MethodGet, "/catalog/bundles/style-nobody-published/manifest", nil, s.token)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for an unpublished bundle, got %d", resp.StatusCode)
		}
	}

	window := observability.Window{
		Start:          before,
		End:            s.metrics.Snapshot(),
		Elapsed:        10 * time.Minute,
		ProcessScraped: true,
	}

	firing := observability.Evaluate(window, 11*time.Minute)
	names := make([]string, 0, len(firing))
	for _, alert := range firing {
		names = append(names, alert.Rule.Name)
	}
	if !contains83(names, "AssetServingFailureSpike") {
		t.Fatalf("AssetServingFailureSpike did not fire on real traffic; firing: %v", names)
	}

	if brief := observability.Evaluate(window, 30*time.Second); len(brief) != 0 {
		t.Errorf("a condition held for 30s should fire nothing, got %v", brief)
	}

	quietStart := s.metrics.Snapshot()
	for i := 0; i < 5; i++ {
		s.do(http.MethodGet, "/catalog/bundles/style-nobody-published/manifest", nil, s.token)
	}
	quiet := observability.Evaluate(observability.Window{
		Start: quietStart, End: s.metrics.Snapshot(),
		Elapsed: 10 * time.Minute, ProcessScraped: true,
	}, time.Hour)
	for _, alert := range quiet {
		if alert.Rule.Name == "AssetServingFailureSpike" {
			t.Errorf("the baseline rate of unpublished-asset 404s must not fire (observed %v)", alert.Observed)
		}
	}
}

func contains83(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// MARK: - Harness

func (s *stack) metricsExposition(t *testing.T) string {
	t.Helper()
	return s.metrics.Expose()
}
