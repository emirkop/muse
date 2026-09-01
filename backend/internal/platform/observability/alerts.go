package observability

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeveritySecurity Severity = "security"
)

type AlertRule struct {
	Name      string
	Severity  Severity
	Signal    Signal
	Threshold float64
	For       time.Duration
	Summary   string
	Runbook   string
}

type Signal string

const (
	SignalProcessUp                   Signal = "process_up"
	SignalDatabaseUp                  Signal = "database_up"
	SignalServerErrorRatio            Signal = "server_error_ratio"
	SignalLatencyP95Seconds           Signal = "latency_p95_seconds"
	SignalEntitlementVerificationRate Signal = "entitlement_verification_failures_per_minute"
	SignalRefreshReuseRate            Signal = "refresh_reuse_per_minute"
	SignalAssetFailureRate            Signal = "asset_delivery_failures_per_minute"
)

var Rules = []AlertRule{
	{
		Name:      "BackendUnavailable",
		Severity:  SeverityCritical,
		Signal:    SignalProcessUp,
		Threshold: 1,
		For:       2 * time.Minute,
		Summary:   "The backend is not reporting as up. No request can be served.",
		Runbook:   "Check the process and the load balancer's target health before anything else.",
	},
	{
		Name:      "ElevatedServerErrorRate",
		Severity:  SeverityHigh,
		Signal:    SignalServerErrorRatio,
		Threshold: 0.05,
		For:       5 * time.Minute,
		Summary:   "More than 5% of responses are 5xx. Requests are failing, not being refused.",
		Runbook:   "Group muse_operational_events_total by category to see which context is failing.",
	},
	{
		Name:      "SustainedLatencyRegression",
		Severity:  SeverityHigh,
		Signal:    SignalLatencyP95Seconds,
		Threshold: 1.0,
		For:       10 * time.Minute,
		Summary:   "95th-percentile latency is above 1s for ten minutes. baseline is sub-millisecond.",
		Runbook:   "Check database connection saturation first; every slow path measured so far was a query.",
	},
	{
		Name:      "DatabaseUnavailable",
		Severity:  SeverityCritical,
		Signal:    SignalDatabaseUp,
		Threshold: 1,
		For:       1 * time.Minute,
		Summary:   "The readiness probe cannot reach the database. Every content route is failing.",
		Runbook:   "Check the database's own health and the connection limit before restarting anything.",
	},
	{
		Name:      "EntitlementVerificationFailing",
		Severity:  SeverityHigh,
		Signal:    SignalEntitlementVerificationRate,
		Threshold: 1.0,
		For:       10 * time.Minute,
		Summary:   "App Store transaction verification is failing or unavailable. Purchases cannot be redeemed.",
		Runbook:   "Distinguish verification_unavailable (Apple's OCSP responder) from verification_failed (ours) in the event label.",
	},
	{
		Name:      "RefreshTokenReuseSpike",
		Severity:  SeveritySecurity,
		Signal:    SignalRefreshReuseRate,
		Threshold: 0.5,
		For:       5 * time.Minute,
		Summary:   "Refresh-token reuse is being detected repeatedly. Token families are being revoked; this may be replay of stolen tokens.",
		Runbook:   "Security response, not uptime response. Correlate by request id in the logs; the token value is deliberately not recorded.",
	},
	{
		Name:      "AssetServingFailureSpike",
		Severity:  SeverityHigh,
		Signal:    SignalAssetFailureRate,
		Threshold: 5.0,
		For:       10 * time.Minute,
		Summary:   "Asset delivery is failing at an elevated rate. Most likely a publish that did not complete.",
		Runbook:   "Check the most recent cmd/assetpublish run; the event label separates not_published from a real failure.",
	},
}

// MARK: - Local evaluation

type Window struct {
	Start          Snapshot
	End            Snapshot
	Elapsed        time.Duration
	ProcessScraped bool
}

type Firing struct {
	Rule     AlertRule
	Observed float64
}

func Evaluate(window Window, held time.Duration) []Firing {
	var firing []Firing
	for _, rule := range Rules {
		value, breached := breach(rule, window)
		if breached && held >= rule.For {
			firing = append(firing, Firing{Rule: rule, Observed: value})
		}
	}
	sort.Slice(firing, func(a, b int) bool { return firing[a].Rule.Name < firing[b].Rule.Name })
	return firing
}

func breach(rule AlertRule, window Window) (float64, bool) {
	switch rule.Signal {
	case SignalProcessUp:
		if !window.ProcessScraped {
			return 0, true
		}
		value := window.End.Gauges[MetricBuildUp]
		return value, value < rule.Threshold

	case SignalDatabaseUp:
		if !window.ProcessScraped {
			return 0, false
		}
		value, present := window.End.Gauges[MetricDatabaseUp]
		if !present {
			return 0, false
		}
		return value, value < rule.Threshold

	case SignalServerErrorRatio:
		total := deltaMatching(window, MetricRequestsTotal, "")
		errors := deltaMatching(window, MetricRequestsTotal, `status="5xx"`)
		if total <= 0 {
			return 0, false
		}
		ratio := errors / total
		return ratio, ratio > rule.Threshold

	case SignalLatencyP95Seconds:
		p95, ok := percentile(window, 0.95)
		if !ok {
			return 0, false
		}
		return p95, p95 > rule.Threshold

	case SignalEntitlementVerificationRate:
		rate := perMinute(window, `category="entitlement"`, `outcome="failed"`) +
			perMinute(window, `category="entitlement"`, `outcome="unavailable"`)
		return rate, rate > rule.Threshold

	case SignalRefreshReuseRate:
		rate := perMinute(window, `event="`+EventRefreshReuseDetected+`"`, "")
		return rate, rate > rule.Threshold

	case SignalAssetFailureRate:
		rate := perMinute(window, `category="asset_delivery"`, "")
		return rate, rate > rule.Threshold
	}
	return 0, false
}

func deltaMatching(window Window, name string, fragments ...string) float64 {
	delta := 0.0
	for key, end := range window.End.Counters {
		if !strings.HasPrefix(key, name) {
			continue
		}
		if !containsAll(key, fragments) {
			continue
		}
		start := window.Start.Counters[key]
		if end >= start {
			delta += end - start
		} else {
			delta += end
		}
	}
	return delta
}

func perMinute(window Window, fragments ...string) float64 {
	if window.Elapsed <= 0 {
		return 0
	}
	return deltaMatching(window, MetricOperationalEvents, fragments...) /
		window.Elapsed.Minutes()
}

func percentile(window Window, quantile float64) (float64, bool) {
	added := make([]uint64, len(LatencyBuckets))
	total := uint64(0)
	for key, end := range window.End.Histograms {
		if !strings.HasPrefix(key, MetricRequestDuration) {
			continue
		}
		start := window.Start.Histograms[key]
		for index := range added {
			endCount := end.Buckets[index]
			var startCount uint64
			if index < len(start.Buckets) {
				startCount = start.Buckets[index]
			}
			if endCount >= startCount {
				added[index] += endCount - startCount
			} else {
				added[index] += endCount
			}
		}
		if end.Count >= start.Count {
			total += end.Count - start.Count
		} else {
			total += end.Count
		}
	}
	if total == 0 {
		return 0, false
	}
	target := float64(total) * quantile
	cumulative := uint64(0)
	for index, bound := range LatencyBuckets {
		cumulative += added[index]
		if float64(cumulative) >= target {
			return bound, true
		}
	}
	return LatencyBuckets[len(LatencyBuckets)-1], true
}

func containsAll(key string, fragments []string) bool {
	for _, fragment := range fragments {
		if fragment == "" {
			continue
		}
		if !strings.Contains(key, fragment) {
			return false
		}
	}
	return true
}

// MARK: - Portable export

func PrometheusRules() string {
	var builder strings.Builder
	builder.WriteString("# GENERATED by internal/platform/observability.PrometheusRules — do not edit by hand.\n")
	builder.WriteString("# Source of truth: observability.Rules.\n")
	builder.WriteString("# Every threshold is an INTERIM ENGINEERING DEFAULT: there is no production\n")
	builder.WriteString("# traffic to calibrate against. is where they meet real load.\n")
	builder.WriteString("groups:\n  - name: muse-backend\n    rules:\n")
	for _, rule := range Rules {
		fmt.Fprintf(&builder, "      - alert: %s\n", rule.Name)
		fmt.Fprintf(&builder, "        expr: %s\n", promExpr(rule))
		fmt.Fprintf(&builder, "        for: %s\n", promDuration(rule.For))
		fmt.Fprintf(&builder, "        labels:\n          severity: %s\n", rule.Severity)
		fmt.Fprintf(&builder, "        annotations:\n")
		fmt.Fprintf(&builder, "          summary: %q\n", rule.Summary)
		fmt.Fprintf(&builder, "          runbook: %q\n", rule.Runbook)
	}
	return builder.String()
}

func promExpr(rule AlertRule) string {
	switch rule.Signal {
	case SignalProcessUp:
		return fmt.Sprintf("%s < 1 or absent(%s)", MetricBuildUp, MetricBuildUp)
	case SignalDatabaseUp:
		return fmt.Sprintf("%s < 1", MetricDatabaseUp)
	case SignalServerErrorRatio:
		return fmt.Sprintf(
			`sum(rate(%s{status="5xx"}[5m])) / clamp_min(sum(rate(%s[5m])), 1) > %g`,
			MetricRequestsTotal, MetricRequestsTotal, rule.Threshold)
	case SignalLatencyP95Seconds:
		return fmt.Sprintf(
			`histogram_quantile(0.95, sum(rate(%s_bucket[10m])) by (le)) > %g`,
			MetricRequestDuration, rule.Threshold)
	case SignalEntitlementVerificationRate:
		return fmt.Sprintf(
			`sum(rate(%s{category="entitlement",outcome=~"failed|unavailable"}[10m])) * 60 > %g`,
			MetricOperationalEvents, rule.Threshold)
	case SignalRefreshReuseRate:
		return fmt.Sprintf(
			`sum(rate(%s{event="%s"}[5m])) * 60 > %g`,
			MetricOperationalEvents, EventRefreshReuseDetected, rule.Threshold)
	case SignalAssetFailureRate:
		return fmt.Sprintf(
			`sum(rate(%s{category="asset_delivery"}[10m])) * 60 > %g`,
			MetricOperationalEvents, rule.Threshold)
	}
	return "vector(0)"
}

func promDuration(d time.Duration) string {
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
