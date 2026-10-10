package http

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// RequestMetric is a route/method/status series with cumulative duration buckets.
// Only declared routes enter labels; an unmatched route is the empty category.
type RequestMetric struct {
	Route   RouteID
	Method  Method
	Result  observability.Result
	Count   uint64
	Seconds float64
	Buckets []uint64
}
type RequestMetricsSnapshot struct {
	Series  []RequestMetric
	Dropped uint64
}
type requestMetricKey struct {
	route  RouteID
	method Method
	result observability.Result
}

// RequestMetrics has a fixed series bound and no per-request I/O. Drain returns
// an owned interval snapshot and resets counters under the same lock as Observe.
type RequestMetrics struct {
	mu      sync.Mutex
	limit   int
	bounds  []time.Duration
	series  map[requestMetricKey]*RequestMetric
	dropped uint64
}

func NewRequestMetrics(maxSeries int) (*RequestMetrics, error) {
	if maxSeries < 1 || maxSeries > 4096 {
		return nil, fault.New(fault.Invalid, "invalid HTTP metric series bound")
	}
	return &RequestMetrics{limit: maxSeries, bounds: observability.DurationBuckets(), series: make(map[requestMetricKey]*RequestMetric)}, nil
}
func (m *RequestMetrics) ObserveRequest(_ context.Context, event RequestEvent) {
	if m == nil {
		return
	}
	result, err := event.Result.Normalized(observability.HTTP)
	if err != nil {
		return
	}
	method := event.Method
	if !method.valid() {
		method = "OTHER"
	}
	route := event.Route
	if route != "" && (len(route) > 256 || strings.IndexAny(string(route), "\r\n\t ") >= 0) {
		route = ""
	}
	key := requestMetricKey{route, method, result}
	m.mu.Lock()
	defer m.mu.Unlock()
	metric := m.series[key]
	if metric == nil {
		if len(m.series) >= m.limit {
			m.dropped++
			return
		}
		metric = &RequestMetric{Route: route, Method: method, Result: result, Buckets: make([]uint64, len(m.bounds))}
		m.series[key] = metric
	}
	metric.Count++
	metric.Seconds += max(0, event.Duration.Seconds())
	for i, bound := range m.bounds {
		if event.Duration <= bound {
			metric.Buckets[i]++
		}
	}
}
func (m *RequestMetrics) snapshot(drain bool) RequestMetricsSnapshot {
	if m == nil {
		return RequestMetricsSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := RequestMetricsSnapshot{Dropped: m.dropped, Series: make([]RequestMetric, 0, len(m.series))}
	for _, metric := range m.series {
		owned := *metric
		owned.Buckets = slices.Clone(metric.Buckets)
		result.Series = append(result.Series, owned)
	}
	slices.SortFunc(result.Series, func(a, b RequestMetric) int {
		if v := strings.Compare(string(a.Route), string(b.Route)); v != 0 {
			return v
		}
		if v := strings.Compare(string(a.Method), string(b.Method)); v != 0 {
			return v
		}
		if a.Result.Status != b.Result.Status {
			return a.Result.Status - b.Result.Status
		}
		return strings.Compare(string(a.Result.Outcome), string(b.Result.Outcome))
	})
	if drain {
		clear(m.series)
		m.dropped = 0
	}
	return result
}
func (m *RequestMetrics) Snapshot() RequestMetricsSnapshot { return m.snapshot(false) }
func (m *RequestMetrics) Drain() RequestMetricsSnapshot    { return m.snapshot(true) }

// WritePrometheus exposes only bounded route metadata. Do not concurrently Drain
// a cumulative exporter: choose snapshots for scraping or drains for persistence.
func (m *RequestMetrics) WritePrometheus(w io.Writer) error {
	snapshot := m.Snapshot()
	if _, err := fmt.Fprintln(w, "# TYPE foundry_http_route_duration_seconds histogram"); err != nil {
		return err
	}
	bounds := observability.DurationBuckets()
	for _, metric := range snapshot.Series {
		labels := fmt.Sprintf("route=%s,method=%s,status=%s,outcome=%s", strconv.Quote(string(metric.Route)), strconv.Quote(string(metric.Method)), strconv.Quote(strconv.Itoa(metric.Result.Status)), strconv.Quote(string(metric.Result.Outcome)))
		for i, bound := range bounds {
			if _, err := fmt.Fprintf(w, "foundry_http_route_duration_seconds_bucket{%s,le=%s} %d\n", labels, strconv.Quote(strconv.FormatFloat(bound.Seconds(), 'g', -1, 64)), metric.Buckets[i]); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "foundry_http_route_duration_seconds_bucket{%s,le=\"+Inf\"} %d\nfoundry_http_route_duration_seconds_count{%s} %d\nfoundry_http_route_duration_seconds_sum{%s} %g\n", labels, metric.Count, labels, metric.Count, labels, metric.Seconds); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "# TYPE foundry_http_route_dropped_total counter\nfoundry_http_route_dropped_total %d\n", snapshot.Dropped)
	return err
}
