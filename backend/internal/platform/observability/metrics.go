package observability

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var LatencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

const (
	MetricRequestsTotal     = "muse_http_requests_total"
	MetricRequestDuration   = "muse_http_request_duration_seconds"
	MetricOperationalEvents = "muse_operational_events_total"
	MetricDatabaseUp        = "muse_database_up"
	MetricBuildUp           = "muse_up"
)

type Registry struct {
	mu         sync.Mutex
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string]*histogram
	series     map[string]labelSet
}

type labelSet struct {
	name   string
	labels [][2]string
}

type histogram struct {
	counts []uint64
	sum    float64
	total  uint64
}

func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]float64{},
		gauges:     map[string]float64{},
		histograms: map[string]*histogram{},
		series:     map[string]labelSet{},
	}
}

func StatusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}

func (r *Registry) ObserveRequest(method, pattern string, statusCode int, elapsed time.Duration) {
	if pattern == "" {
		pattern = "unmatched"
	}
	r.incr(MetricRequestsTotal, [][2]string{
		{"method", method},
		{"route", pattern},
		{"status", StatusClass(statusCode)},
	}, 1)
	r.observe(MetricRequestDuration, [][2]string{{"route", pattern}}, elapsed.Seconds())
}

func (r *Registry) ObserveEvent(event string, category Category, outcome Outcome) {
	r.incr(MetricOperationalEvents, [][2]string{
		{"event", event},
		{"category", string(category)},
		{"outcome", string(outcome)},
	}, 1)
}

func (r *Registry) SetDatabaseUp(up bool) {
	value := 0.0
	if up {
		value = 1
	}
	r.set(MetricDatabaseUp, nil, value)
}

func (r *Registry) MarkUp() {
	r.set(MetricBuildUp, nil, 1)
}

type Snapshot struct {
	Counters   map[string]float64
	Gauges     map[string]float64
	Histograms map[string]HistogramSnapshot
}

type HistogramSnapshot struct {
	Buckets []uint64
	Sum     float64
	Count   uint64
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := Snapshot{
		Counters:   make(map[string]float64, len(r.counters)),
		Gauges:     make(map[string]float64, len(r.gauges)),
		Histograms: make(map[string]HistogramSnapshot, len(r.histograms)),
	}
	for key, value := range r.counters {
		out.Counters[key] = value
	}
	for key, value := range r.gauges {
		out.Gauges[key] = value
	}
	for key, h := range r.histograms {
		buckets := make([]uint64, len(h.counts))
		copy(buckets, h.counts)
		out.Histograms[key] = HistogramSnapshot{Buckets: buckets, Sum: h.sum, Count: h.total}
	}
	return out
}

func (r *Registry) Expose() string {
	snapshot := r.Snapshot()

	r.mu.Lock()
	series := make(map[string]labelSet, len(r.series))
	for key, set := range r.series {
		series[key] = set
	}
	r.mu.Unlock()

	var builder strings.Builder
	writeHelp := map[string]bool{}

	keys := sortedKeys(snapshot.Gauges)
	for _, key := range keys {
		set := series[key]
		emitHeader(&builder, writeHelp, set.name, "gauge")
		fmt.Fprintf(&builder, "%s %s\n", key, formatFloat(snapshot.Gauges[key]))
	}
	for _, key := range sortedKeys(snapshot.Counters) {
		set := series[key]
		emitHeader(&builder, writeHelp, set.name, "counter")
		fmt.Fprintf(&builder, "%s %s\n", key, formatFloat(snapshot.Counters[key]))
	}
	for _, key := range sortedHistogramKeys(snapshot.Histograms) {
		set := series[key]
		emitHeader(&builder, writeHelp, set.name, "histogram")
		h := snapshot.Histograms[key]
		cumulative := uint64(0)
		for index, bound := range LatencyBuckets {
			cumulative += h.Buckets[index]
			fmt.Fprintf(&builder, "%s_bucket%s %d\n",
				set.name, withExtraLabel(set.labels, "le", formatFloat(bound)), cumulative)
		}
		fmt.Fprintf(&builder, "%s_bucket%s %d\n",
			set.name, withExtraLabel(set.labels, "le", "+Inf"), h.Count)
		fmt.Fprintf(&builder, "%s_sum%s %s\n", set.name, renderLabels(set.labels), formatFloat(h.Sum))
		fmt.Fprintf(&builder, "%s_count%s %d\n", set.name, renderLabels(set.labels), h.Count)
	}
	return builder.String()
}

// MARK: - Internals

func (r *Registry) incr(name string, labels [][2]string, delta float64) {
	key := seriesKey(name, labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[key] += delta
	r.series[key] = labelSet{name: name, labels: labels}
}

func (r *Registry) set(name string, labels [][2]string, value float64) {
	key := seriesKey(name, labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[key] = value
	r.series[key] = labelSet{name: name, labels: labels}
}

func (r *Registry) observe(name string, labels [][2]string, value float64) {
	key := seriesKey(name, labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.histograms[key]
	if !ok {
		h = &histogram{counts: make([]uint64, len(LatencyBuckets))}
		r.histograms[key] = h
		r.series[key] = labelSet{name: name, labels: labels}
	}
	h.sum += value
	h.total++
	for index, bound := range LatencyBuckets {
		if value <= bound {
			h.counts[index]++
			break
		}
	}
}

func seriesKey(name string, labels [][2]string) string {
	return name + renderLabels(labels)
}

func renderLabels(labels [][2]string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, fmt.Sprintf("%s=%q", label[0], label[1]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func withExtraLabel(labels [][2]string, name, value string) string {
	extended := make([][2]string, 0, len(labels)+1)
	extended = append(extended, labels...)
	extended = append(extended, [2]string{name, value})
	return renderLabels(extended)
}

func emitHeader(builder *strings.Builder, written map[string]bool, name, kind string) {
	if written[name] {
		return
	}
	written[name] = true
	fmt.Fprintf(builder, "# TYPE %s %s\n", name, kind)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedHistogramKeys(m map[string]HistogramSnapshot) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
