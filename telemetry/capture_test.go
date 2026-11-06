package telemetry

import (
	"math"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type clockFunc func() time.Time

func (f clockFunc) Now() time.Time { return f() }

func TestClockFailuresPreserveUncapturedInput(t *testing.T) {
	for name, source := range map[string]clock.Clock{
		"panic":          clockFunc(func() time.Time { panic("private-clock-payload") }),
		"goexit":         clockFunc(func() time.Time { runtime.Goexit(); return time.Time{} }),
		"uuid range":     clockFunc(func() time.Time { return time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC) }),
		"datetime range": clockFunc(func() time.Time { return time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }),
		"nil":            nil,
		"typed nil":      (*testClock)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCollector(collectorConfig(), source); err == nil || strings.Contains(err.Error(), "private-clock-payload") {
				t.Fatal("constructor did not contain clock failure")
			}
			c, good := newCollector(t, collectorConfig())
			c.ObserveRequest(t.Context(), request())
			if err := c.Report(t.Context(), backendReport()); err != nil {
				t.Fatal(err)
			}
			c.clock = source
			if c.capture(nil) == nil {
				t.Fatal("capture accepted invalid clock")
			}
			if len(c.pending) != 0 {
				t.Fatal("failed capture queued a batch")
			}
			c.clock = good
			b := capture(t, c)
			if len(b.issueSnapshot()) != 1 || len(b.bucketSnapshot()) != 2 || b.bucketSnapshot()[0].Metric.Count != 1 || b.runtimeSnapshot().Dropped != 0 {
				t.Fatal("clock failure drained admitted observations")
			}
		})
	}
}

func TestObservationClockFailuresCountLoss(t *testing.T) {
	c, good := newCollector(t, collectorConfig())
	c.clock = clockFunc(func() time.Time { panic("secret") })
	if c.Report(t.Context(), backendReport()) == nil {
		t.Fatal("backend clock failure hidden")
	}
	if c.RecordBrowser(t.Context(), browserReport()) == nil {
		t.Fatal("browser clock failure hidden")
	}
	c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Path: "/.env"})
	c.clock = good
	b := capture(t, c)
	if b.runtimeSnapshot().Dropped != 3 || len(b.issueSnapshot()) != 0 {
		t.Fatal("failed timestamps were admitted or not counted")
	}
}

func TestPendingExpiryIgnoresApplicationClockJumps(t *testing.T) {
	config := collectorConfig()
	config.PendingRetention = time.Minute
	c, wall := newCollector(t, config)
	elapsed := &testClock{}
	elapsed.set(wall.Now())
	c.elapsed = elapsed.Now
	c.ObserveRequest(t.Context(), request())
	first := capture(t, c)
	wall.advance(24 * time.Hour)
	capture(t, c)
	if c.pending[0].id != first.id || c.dropped != 0 {
		t.Fatal("wall-clock jump expired pending observations")
	}
	wall.advance(-48 * time.Hour)
	elapsed.advance(time.Minute)
	latest := capture(t, c)
	if len(c.pending) != 1 || c.pending[0].id == first.id || latest.runtimeSnapshot().Dropped != 1 {
		t.Fatal("wall-clock rollback extended the admitted retry lifetime")
	}
}

func TestConfigurationSnapshotPrecedesInjectedClock(t *testing.T) {
	config := collectorConfig()
	clock := clockFunc(func() time.Time {
		config.Browser.Portals[0] = "../invalid"
		return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	})
	c, err := NewCollector(config, clock)
	if err != nil {
		t.Fatal(err)
	}
	if c.config.Browser.Portals[0] != "admin" {
		t.Fatal("an injected dependency changed validated configuration")
	}
}

func TestRuntimeSampleBoundsAndSnapshotOwnership(t *testing.T) {
	input := make([]observability.MetricSample, maxRuntimeSamples+2)
	for i := range input {
		input[i] = observability.MetricSample{Name: "runtime_gauge", Labels: "role=\"worker\"", Value: 1}
	}
	input[0].Labels = strings.Repeat("x", maxSampleLabelBytes+1)
	input[1].Labels = strings.Repeat("x", maxSampleLabelBytes)
	samples, dropped := captureSamples(input)
	if len(samples) != maxRuntimeSamples || dropped != 2 || len(samples[0].Labels) != maxSampleLabelBytes {
		t.Fatal("runtime sample or label bounds changed")
	}
	input[1].Value = 2
	input[1].Labels = "changed"
	b := batch{runtime: Runtime{Samples: samples, Health: Health{Dependencies: []DependencyHealth{{ID: "db"}}}}}
	owned := b.runtimeSnapshot()
	owned.Samples[0].Value = 3
	owned.Health.Dependencies[0].ID = "changed"
	if b.runtimeSnapshot().Samples[0].Value != 1 || b.runtimeSnapshot().Health.Dependencies[0].ID != "db" {
		t.Fatal("runtime snapshot aliased retained data")
	}
}

func TestRecorderSamplesAndCallbackIsolation(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	r, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterCollector("test.gauge", func(w *observability.MetricWriter) {
		w.Gauge("telemetry_test_gauge", "test", 7, observability.Label{Name: "role", Value: "worker"})
		w.Gauge("telemetry_invalid_gauge", "test", math.NaN())
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterCollector("test.panic", func(*observability.MetricWriter) { panic("private-collector-payload") }); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterCollector("test.goexit", func(*observability.MetricWriter) { runtime.Goexit() }); err != nil {
		t.Fatal(err)
	}
	if err := c.capture(r); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sample := range c.pending[0].runtimeSnapshot().Samples {
		if sample.Name == "telemetry_test_gauge" {
			found = true
			if sample.Value != 7 || sample.Labels != "{role=\"worker\"}" {
				t.Fatal("recorder sample changed")
			}
		}
		if sample.Name == "telemetry_invalid_gauge" || strings.Contains(sample.Labels, "private-collector-payload") {
			t.Fatal("invalid sample or callback payload retained")
		}
	}
	if !found {
		t.Fatal("did not use recorder samples")
	}
	if c.failures != 0 {
		t.Fatal("recorder callback failure treated as storage failure")
	}
}

func TestCaptureDoesNotHoldAdmissionLockDuringRecorderCallbacks(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	r, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	if err := r.RegisterCollector("test.wait", func(*observability.MetricWriter) { close(entered); <-release }); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.capture(r) }()
	<-entered
	observed := make(chan struct{})
	go func() { c.ObserveRequest(t.Context(), request()); close(observed) }()
	// Only this deadlock guard uses real time; all capture semantics use testClock.
	select {
	case <-observed:
	case <-time.After(5 * time.Second):
		close(release)
		<-done
		t.Fatal("recorder callback held admission lock")
	}
	select {
	case <-done:
		close(release)
		t.Fatal("capture abandoned live callback")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if c.pending[0].bucketSnapshot()[0].Metric.Count != 1 {
		t.Fatal("concurrent admission lost")
	}
}

func TestCumulativeOverflowRejectsWholeDeltaAndLossSaturates(t *testing.T) {
	for _, kind := range []string{"count", "histogram", "seconds"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := newCollector(t, collectorConfig())
			c.ObserveRequest(t.Context(), request())
			capture(t, c)
			for key, b := range c.buckets {
				switch kind {
				case "count":
					b.Metric.Count = math.MaxUint64
				case "histogram":
					for i := range b.Metric.Buckets {
						b.Metric.Buckets[i] = math.MaxUint64
					}
				case "seconds":
					b.Metric.Seconds = math.MaxFloat64
				}
				c.buckets[key] = b
			}
			before := make(map[string]http.RequestMetric)
			for key, b := range c.buckets {
				b.Metric.Buckets = slices.Clone(b.Metric.Buckets)
				before[key] = b.Metric
			}
			if kind == "seconds" {
				// The HTTP owner emits finite durations. Exercise the cumulative
				// addition directly with a finite imported interval near float max.
				metric := requestMetric()
				metric.Seconds = math.MaxFloat64
				for _, interval := range []time.Duration{time.Minute, time.Hour} {
					c.mergeLocked(c.started.UTC(), interval, metric)
				}
			} else {
				c.ObserveRequest(t.Context(), request())
				capture(t, c)
			}
			for key, b := range c.buckets {
				if !reflect.DeepEqual(before[key], b.Metric) {
					t.Fatal("overflow partially changed histogram")
				}
			}
			if c.dropped != 2 {
				t.Fatalf("overflow loss = %d", c.dropped)
			}
			c.dropped = math.MaxUint64 - 1
			c.drop(2)
			if c.dropped != math.MaxUint64 {
				t.Fatal("loss counter wrapped")
			}
		})
	}
}

func requestMetric() http.RequestMetric {
	e := request()
	result, _ := e.Result.Normalized(observability.HTTP)
	return http.RequestMetric{Route: e.Route, Method: e.Method, Result: result, Count: 1, Buckets: make([]uint64, len(observability.DurationBuckets()))}
}

func TestHistogramOverflowUsesFrameworkEstimator(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	e := request()
	e.Duration = 24 * time.Hour
	c.ObserveRequest(t.Context(), e)
	for _, b := range capture(t, c).bucketSnapshot() {
		estimate, err := observability.EstimateQuantile(b.Metric.Buckets, b.Metric.Count, .95)
		if err != nil || !estimate.Available || !estimate.Overflow {
			t.Fatalf("long request quantile = %+v, %v", estimate, err)
		}
	}
}

func TestIndependentMetricsOwnerAndCollectors(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	other, _ := newCollector(t, collectorConfig())
	exporter, err := http.NewRequestMetrics(16)
	if err != nil {
		t.Fatal(err)
	}
	exporter.ObserveRequest(t.Context(), request())
	c.ObserveRequest(t.Context(), request())
	capture(t, c)
	if exporter.Snapshot().Series[0].Count != 1 || len(capture(t, other).bucketSnapshot()) != 0 || c.RunID() == other.RunID() {
		t.Fatal("collectors share metrics or identity")
	}
}

func TestSealedAdmissionCountsLateReportsWithoutReopening(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	if err := c.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	c.seal()
	c.seal()
	b := capture(t, c)
	c.ObserveRequest(t.Context(), request())
	c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Path: "/.env"})
	if err := c.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	if err := c.RecordBrowser(t.Context(), browserReport()); err != nil {
		t.Fatal(err)
	}
	if c.dropped != 4 || len(c.issues) != 0 || len(c.pending) != 1 || len(b.issueSnapshot()) != 1 || b.runtimeSnapshot().Dropped != 0 {
		t.Fatal("late admission reopened or mutated final capture")
	}
}
