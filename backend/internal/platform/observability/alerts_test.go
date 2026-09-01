package observability

import (
	"os"
	"strings"
	"testing"
	"time"
)

func scenario(before, after *Registry, elapsed time.Duration) Window {
	return Window{
		Start:          before.Snapshot(),
		End:            after.Snapshot(),
		Elapsed:        elapsed,
		ProcessScraped: true,
	}
}

func firingNames(firing []Firing) []string {
	names := make([]string, 0, len(firing))
	for _, f := range firing {
		names = append(names, f.Rule.Name)
	}
	return names
}

func assertFires(t *testing.T, window Window, held time.Duration, want string) Firing {
	t.Helper()
	for _, f := range Evaluate(window, held) {
		if f.Rule.Name == want {
			return f
		}
	}
	t.Fatalf("%s did not fire; firing: %v", want, firingNames(Evaluate(window, held)))
	return Firing{}
}

func assertQuiet(t *testing.T, window Window, held time.Duration, unwanted string) {
	t.Helper()
	for _, f := range Evaluate(window, held) {
		if f.Rule.Name == unwanted {
			t.Fatalf("%s fired when it should not have (observed %v)", unwanted, f.Observed)
		}
	}
}

func TestAlerts_AHealthySystemIsQuiet(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	for i := 0; i < 500; i++ {
		after.ObserveRequest("GET", "GET /museum/me", 200, 900*time.Microsecond)
	}
	for i := 0; i < 30; i++ {
		after.ObserveRequest("GET", "GET /museum/me/rooms/{roomID}", 404, 800*time.Microsecond)
		after.ObserveEvent(EventAuthorizationRefused, CategoryAuthz, OutcomeRefused)
	}
	for i := 0; i < 5; i++ {
		after.ObserveEvent(EventAssetBundleNotPublished, CategoryAssetDelivery, OutcomeRefused)
	}

	firing := Evaluate(scenario(before, after, 10*time.Minute), time.Hour)
	if len(firing) != 0 {
		t.Fatalf("a healthy system fired %v", firingNames(firing))
	}
}

func TestAlerts_BackendUnavailableFiresWhenTheScrapeFails(t *testing.T) {
	window := Window{Start: NewRegistry().Snapshot(), End: NewRegistry().Snapshot(),
		Elapsed: time.Minute, ProcessScraped: false}

	firing := assertFires(t, window, 3*time.Minute, "BackendUnavailable")
	if firing.Rule.Severity != SeverityCritical {
		t.Errorf("severity = %v, want critical", firing.Rule.Severity)
	}
	assertQuiet(t, window, 30*time.Second, "BackendUnavailable")

	assertQuiet(t, window, 3*time.Minute, "DatabaseUnavailable")
}

func TestAlerts_ElevatedServerErrorRate(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	for i := 0; i < 90; i++ {
		after.ObserveRequest("GET", "GET /museum/me", 200, time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		after.ObserveRequest("GET", "GET /museum/me", 500, time.Millisecond)
	}
	window := scenario(before, after, 5*time.Minute)

	firing := assertFires(t, window, 6*time.Minute, "ElevatedServerErrorRate")
	if firing.Observed <= 0.05 {
		t.Errorf("observed ratio %v should exceed the 5%% threshold", firing.Observed)
	}
	assertQuiet(t, window, 1*time.Minute, "ElevatedServerErrorRate")
}

func TestAlerts_RefusalsAreNotAnErrorRate(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	for i := 0; i < 1000; i++ {
		after.ObserveRequest("GET", "GET /museum/me/rooms/{roomID}", 404, time.Millisecond)
		after.ObserveRequest("POST", "POST /analytics/events", 400, time.Millisecond)
	}
	assertQuiet(t, scenario(before, after, 5*time.Minute), time.Hour, "ElevatedServerErrorRate")
}

func TestAlerts_NoTrafficIsNotAnErrorRate(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	assertQuiet(t, scenario(before, after, 5*time.Minute), time.Hour, "ElevatedServerErrorRate")
}

func TestAlerts_SustainedLatencyRegression(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	for i := 0; i < 90; i++ {
		after.ObserveRequest("GET", "GET /museum/me", 200, 2*time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		after.ObserveRequest("GET", "GET /museum/me", 200, 4*time.Second)
	}
	window := scenario(before, after, 10*time.Minute)

	firing := assertFires(t, window, 11*time.Minute, "SustainedLatencyRegression")
	if firing.Observed <= 1.0 {
		t.Errorf("observed p95 %v should exceed 1s", firing.Observed)
	}
	assertQuiet(t, window, 2*time.Minute, "SustainedLatencyRegression")

	fast := NewRegistry()
	fast.MarkUp()
	fast.SetDatabaseUp(true)
	for i := 0; i < 1000; i++ {
		fast.ObserveRequest("GET", "GET /museum/me", 200, 200*time.Microsecond)
	}
	assertQuiet(t, scenario(before, fast, 10*time.Minute), time.Hour, "SustainedLatencyRegression")
}

func TestAlerts_DatabaseUnavailable(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(false)
	window := scenario(before, after, time.Minute)

	firing := assertFires(t, window, 2*time.Minute, "DatabaseUnavailable")
	if firing.Rule.Severity != SeverityCritical {
		t.Errorf("severity = %v, want critical", firing.Rule.Severity)
	}
	assertQuiet(t, window, 10*time.Second, "DatabaseUnavailable")

	neverProbed := NewRegistry()
	neverProbed.MarkUp()
	assertQuiet(t, scenario(before, neverProbed, time.Minute), time.Hour, "DatabaseUnavailable")
}

func TestAlerts_EntitlementVerificationFailing(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	for i := 0; i < 30; i++ {
		after.ObserveEvent(EventEntitlementVerificationUnavailable, CategoryEntitlement, OutcomeUnavailable)
	}
	window := scenario(before, after, 10*time.Minute)
	assertFires(t, window, 11*time.Minute, "EntitlementVerificationFailing")

	refusals := NewRegistry()
	refusals.MarkUp()
	refusals.SetDatabaseUp(true)
	for i := 0; i < 300; i++ {
		refusals.ObserveEvent(EventEntitlementNotApplicable, CategoryEntitlement, OutcomeRefused)
	}
	assertQuiet(t, scenario(before, refusals, 10*time.Minute), time.Hour, "EntitlementVerificationFailing")
}

func TestAlerts_RefreshTokenReuseSpike(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	for i := 0; i < 20; i++ {
		after.ObserveEvent(EventRefreshReuseDetected, CategoryAuthn, OutcomeRefused)
	}
	window := scenario(before, after, 5*time.Minute)

	firing := assertFires(t, window, 6*time.Minute, "RefreshTokenReuseSpike")
	if firing.Rule.Severity != SeveritySecurity {
		t.Fatalf("severity = %v, want security — this routes to a different responder than an outage",
			firing.Rule.Severity)
	}

	single := NewRegistry()
	single.MarkUp()
	single.SetDatabaseUp(true)
	single.ObserveEvent(EventRefreshReuseDetected, CategoryAuthn, OutcomeRefused)
	assertQuiet(t, scenario(before, single, 5*time.Minute), time.Hour, "RefreshTokenReuseSpike")

	logins := NewRegistry()
	logins.MarkUp()
	logins.SetDatabaseUp(true)
	for i := 0; i < 500; i++ {
		logins.ObserveEvent(EventLoginRefused, CategoryAuthn, OutcomeRefused)
	}
	assertQuiet(t, scenario(before, logins, 5*time.Minute), time.Hour, "RefreshTokenReuseSpike")
}

func TestAlerts_AssetServingFailureSpike(t *testing.T) {
	before, after := NewRegistry(), NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	for i := 0; i < 100; i++ {
		after.ObserveEvent(EventAssetPublishFailed, CategoryAssetDelivery, OutcomeFailed)
	}
	window := scenario(before, after, 10*time.Minute)
	assertFires(t, window, 11*time.Minute, "AssetServingFailureSpike")

	baseline := NewRegistry()
	baseline.MarkUp()
	baseline.SetDatabaseUp(true)
	for i := 0; i < 20; i++ {
		baseline.ObserveEvent(EventAssetBundleNotPublished, CategoryAssetDelivery, OutcomeRefused)
	}
	assertQuiet(t, scenario(before, baseline, 10*time.Minute), time.Hour, "AssetServingFailureSpike")
}

func TestAlerts_ACounterResetDoesNotProduceANegativeRate(t *testing.T) {
	before := NewRegistry()
	for i := 0; i < 1000; i++ {
		before.ObserveEvent(EventRefreshReuseDetected, CategoryAuthn, OutcomeRefused)
	}
	after := NewRegistry()
	after.MarkUp()
	after.SetDatabaseUp(true)
	after.ObserveEvent(EventRefreshReuseDetected, CategoryAuthn, OutcomeRefused)

	assertQuiet(t, scenario(before, after, 5*time.Minute), time.Hour, "RefreshTokenReuseSpike")
}

func TestAlerts_EveryRuleIsCoveredBySomeSimulation(t *testing.T) {
	covered := map[string]bool{}
	for _, name := range []string{
		"BackendUnavailable", "ElevatedServerErrorRate", "SustainedLatencyRegression",
		"DatabaseUnavailable", "EntitlementVerificationFailing", "RefreshTokenReuseSpike",
		"AssetServingFailureSpike",
	} {
		covered[name] = true
	}
	for _, rule := range Rules {
		if !covered[rule.Name] {
			t.Errorf("rule %q has no simulation in this file", rule.Name)
		}
		if rule.For <= 0 {
			t.Errorf("rule %q has no `for` duration — it will fire on a single blip", rule.Name)
		}
		if rule.Summary == "" || rule.Runbook == "" {
			t.Errorf("rule %q has no summary or runbook — an alert nobody can act on trains people to ignore alerts", rule.Name)
		}
	}
	if len(Rules) != len(covered) {
		t.Fatalf("Rules holds %d rules, this file simulates %d", len(Rules), len(covered))
	}
}

func TestPrometheusRules_RendersEveryRuleFromTheSameSource(t *testing.T) {
	rendered := PrometheusRules()
	for _, rule := range Rules {
		if !strings.Contains(rendered, "alert: "+rule.Name) {
			t.Errorf("%s is missing from the exported rules", rule.Name)
		}
		if !strings.Contains(rendered, "severity: "+string(rule.Severity)) {
			t.Errorf("%s's severity is missing", rule.Name)
		}
	}
	if !strings.Contains(rendered, "INTERIM ENGINEERING DEFAULT") {
		t.Error("the exported rules must state that the thresholds are untuned")
	}
	if !strings.Contains(rendered, "GENERATED") {
		t.Error("the exported rules must say they are generated")
	}
	for _, line := range strings.Split(rendered, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "expr:") {
			continue
		}
		for _, forbidden := range []string{
			"account_id=", "room_id=", "museum_id=", "asset_id=", "photo_asset_id=",
			"collection_room_id=", "code=", "token=", "key=", "path=",
		} {
			if strings.Contains(trimmed, forbidden) {
				t.Errorf("expression %q references label %q — no metric carries one", trimmed, forbidden)
			}
		}
	}
}

func TestPrometheusRules_TheCommittedArtefactIsUpToDate(t *testing.T) {
	committed, err := os.ReadFile("../../../deploy/monitoring/muse-alerts.yml")
	if err != nil {
		t.Fatalf("the generated rules are missing: %v", err)
	}
	if string(committed) != PrometheusRules() {
		t.Fatal("deploy/monitoring/muse-alerts.yml is stale — regenerate it with `go run ./cmd/alertrules`")
	}
}
