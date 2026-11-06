package telemetry

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type testClock struct{ nanos atomic.Int64 }

func (c *testClock) Now() time.Time          { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *testClock) set(now time.Time)       { c.nanos.Store(now.UnixNano()) }
func (c *testClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

func collectorConfig() CollectorConfig {
	return CollectorConfig{Instance: "test", Release: "development", MaxSeries: 16, MaxEvents: 16, MaxPendingBatches: 6, PendingRetention: time.Hour, Browser: BrowserPolicy{Enabled: true, Portals: []Portal{"admin", "user"}}}
}

func newCollector(t *testing.T, config CollectorConfig) (*Collector, *testClock) {
	t.Helper()
	clock := &testClock{}
	clock.set(time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC))
	c, err := NewCollector(config, clock)
	if err != nil {
		t.Fatal(err)
	}
	c.elapsed = clock.Now
	return c, clock
}

func capture(t *testing.T, c *Collector) batch {
	t.Helper()
	if err := c.capture(nil); err != nil {
		t.Fatal(err)
	}
	if len(c.pending) == 0 {
		t.Fatal("capture produced no pending batch")
	}
	return c.pending[len(c.pending)-1]
}

func request() http.RequestEvent {
	return http.RequestEvent{Route: "example.read", Method: http.GET, Duration: 10 * time.Millisecond, Result: observability.Result{Status: 200}}
}

func backendReport() observability.ErrorReport {
	return observability.ErrorReport{Entry: observability.Entry{Operation: observability.Operation{Kind: observability.HTTP, Name: "example.read"}, Route: attribution.Route{Name: "example.read"}}, Diagnostic: fault.Diagnostic{Types: []string{"*example.Failure"}}}
}

func browserReport() BrowserError {
	return BrowserError{Portal: "admin", Release: "development", Code: BrowserCodeTypeError, Frames: []BrowserFrame{{File: "admin/_assets/app.js", Line: 1}}}
}

func TestSecurityDiagnosticsDiscardArbitraryPathsAndQueries(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	for _, path := range []string{"/accounts/email-canary?token=password-canary", "/.env/password-canary"} {
		c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Path: path, ClientIP: netip.MustParseAddr("192.0.2.8"), Request: http.RequestEvent{Result: observability.Result{Status: 404}}})
	}
	data, err := json.Marshal(capture(t, c).issueSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "canary") || strings.Contains(string(data), "?token=") {
		t.Fatal("security sample retained a submitted path")
	}
	if !strings.Contains(string(data), "192.0.2.8") || !strings.Contains(string(data), "environment_probe") {
		t.Fatal("trusted address or structural evidence lost")
	}
}

func TestCapturedMetricsAreStableAndOutageQueuesStayBounded(t *testing.T) {
	config := collectorConfig()
	config.MaxPendingBatches = 1
	c, _ := newCollector(t, config)
	c.ObserveRequest(t.Context(), request())
	first := capture(t, c)
	before := first.bucketSnapshot()
	if len(before) != 2 || before[0].Metric.Count != 1 {
		t.Fatal("missing minute/hour histograms")
	}
	for range 10 {
		c.ObserveRequest(t.Context(), request())
		c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Path: "/.git/config", Request: request()})
		capture(t, c)
	}
	if !reflect.DeepEqual(first.bucketSnapshot(), before) || c.pending[0].id != first.id {
		t.Fatal("pending snapshot or receipt identity changed")
	}
	if len(c.pending) != 1 || c.dropped != 20 || c.failures != 0 {
		t.Fatalf("queue/loss = %d/%d/%d", len(c.pending), c.dropped, c.failures)
	}
}

func TestBrowserAdmissionCannotFillBackendShareOrInventReleases(t *testing.T) {
	config := collectorConfig()
	config.MaxEvents = 8
	c, _ := newCollector(t, config)
	report := browserReport()
	for range 20 {
		if err := c.RecordBrowser(t.Context(), report); err != nil {
			t.Fatal(err)
		}
	}
	if c.issueCounts[SourceBrowser] != 2 || c.dropped != 18 {
		t.Fatal("browser share or loss accounting differs")
	}
	if err := c.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	if c.issueCounts[SourceBackend] != 1 {
		t.Fatal("browser reports starved backend admission")
	}
	report.Release = "invented-release"
	if c.RecordBrowser(t.Context(), report) == nil {
		t.Fatal("accepted arbitrary release")
	}
}

func TestEveryIssueShareAndTotalCapacity(t *testing.T) {
	config := collectorConfig()
	config.MaxEvents = 8
	c, _ := newCollector(t, config)
	for range 3 {
		if err := c.RecordBrowser(t.Context(), browserReport()); err != nil {
			t.Fatal(err)
		}
	}
	for range 5 {
		c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Path: "/.git/config"})
	}
	for range 3 {
		if err := c.Report(t.Context(), backendReport()); err != nil {
			t.Fatal(err)
		}
	}
	b := capture(t, c)
	counts := map[Source]int{}
	for _, issue := range b.issueSnapshot() {
		counts[issue.Source]++
	}
	if !reflect.DeepEqual(counts, map[Source]int{SourceBrowser: 2, SourceSecurity: 4, SourceBackend: 2}) || b.runtimeSnapshot().Dropped != 3 {
		t.Fatalf("counts/loss = %v/%d", counts, b.runtimeSnapshot().Dropped)
	}
	if err := c.RecordBrowser(t.Context(), browserReport()); err != nil {
		t.Fatal(err)
	}
	if len(capture(t, c).issueSnapshot()) != 1 {
		t.Fatal("capture did not release source capacity")
	}
	for _, capacity := range []int{1, 2, 3} {
		config.MaxEvents = capacity
		small, _ := newCollector(t, config)
		if err := small.RecordBrowser(t.Context(), browserReport()); err != nil {
			t.Fatal(err)
		}
		if err := small.Report(t.Context(), backendReport()); err != nil {
			t.Fatal(err)
		}
		if small.issueCounts[SourceBrowser] != 0 || small.issueCounts[SourceBackend] != 1 || small.dropped != 1 {
			t.Fatal("floor shares did not preserve backend capacity")
		}
	}
}

func TestSeriesAndCumulativeBucketCapacity(t *testing.T) {
	config := collectorConfig()
	config.MaxSeries = 1
	c, clock := newCollector(t, config)
	first, second := request(), request()
	second.Route = "other.read"
	c.ObserveRequest(t.Context(), first)
	c.ObserveRequest(t.Context(), second)
	if b := capture(t, c); b.runtimeSnapshot().Dropped != 1 || len(b.bucketSnapshot()) != 2 {
		t.Fatal("HTTP series capacity not counted")
	}
	c.ObserveRequest(t.Context(), second)
	if b := capture(t, c); b.runtimeSnapshot().Dropped != 3 || len(b.bucketSnapshot()) != 2 {
		t.Fatal("minute/hour bucket admission not bounded")
	}
	clock.advance(time.Hour)
	c.ObserveRequest(t.Context(), second)
	b := capture(t, c)
	for _, entry := range b.bucketSnapshot() {
		if entry.Metric.Route != second.Route || entry.Metric.Count != 1 {
			t.Fatal("expired buckets retained capacity")
		}
	}
}

func TestCaptureBoundariesAndDuplicateInputs(t *testing.T) {
	c, clock := newCollector(t, collectorConfig())
	clock.set(time.Date(2026, 11, 6, 12, 59, 59, 999999999, time.FixedZone("offset", 3600)))
	c.ObserveRequest(t.Context(), request())
	first := capture(t, c)
	if first.at.UTC().Nanosecond()%1000 != 0 || first.id.IsZero() || c.RunID().IsZero() {
		t.Fatal("clock precision or UUID missing")
	}
	duplicate := capture(t, c)
	if duplicate.id == first.id || !reflect.DeepEqual(first.bucketSnapshot(), duplicate.bucketSnapshot()) {
		t.Fatal("capture repeated deltas or reused identity")
	}
	clock.advance(time.Nanosecond)
	c.ObserveRequest(t.Context(), request())
	boundary := capture(t, c)
	for _, entry := range boundary.bucketSnapshot() {
		if !entry.At.Equal(clock.Now().Truncate(entry.Interval)) || entry.Metric.Count != 1 {
			t.Fatal("hour boundary did not start new buckets")
		}
	}
	clock.advance(time.Minute)
	c.ObserveRequest(t.Context(), request())
	minute := capture(t, c)
	for _, entry := range minute.bucketSnapshot() {
		want := uint64(1)
		if entry.Interval == time.Hour {
			want = 2
		}
		if entry.Metric.Count != want {
			t.Fatalf("%s count = %d, want %d", entry.Interval, entry.Metric.Count, want)
		}
	}
	if len(minute.issueSnapshot()) != 0 {
		t.Fatal("empty issue input invented observations")
	}
}

func TestPendingExpiryCountsCumulativeMinuteSnapshotsOnce(t *testing.T) {
	config := collectorConfig()
	config.PendingRetention = time.Minute
	config.MaxPendingBatches = 2
	c, clock := newCollector(t, config)
	c.ObserveRequest(t.Context(), request())
	if err := c.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	first := capture(t, c)
	clock.advance(30 * time.Second)
	c.ObserveRequest(t.Context(), request())
	second := capture(t, c)
	clock.advance(30 * time.Second)
	third := capture(t, c)
	if len(c.pending) != 2 || c.pending[0].id != second.id || third.runtimeSnapshot().Dropped != 2 {
		t.Fatal("expiry did not free head/count issue plus minute")
	}
	clock.advance(30 * time.Second)
	fourth := capture(t, c)
	if fourth.runtimeSnapshot().Dropped != 4 || first.runtimeSnapshot().Dropped != 0 || second.runtimeSnapshot().Dropped != 0 {
		t.Fatal("cumulative expiry count or immutable runtime changed")
	}
	if c.failures != 0 {
		t.Fatal("expiry counted as storage failure")
	}
}

func TestConfigurationRoutesAndIssueSnapshotsAreOwned(t *testing.T) {
	config := collectorConfig()
	config.Routes = RoutePolicy{ExcludedRoutes: []http.RouteID{"excluded"}, ExcludedPrefixes: []string{"health."}, ExcludedOperations: []observability.Name{"skip"}, ExcludedOperationPrefixes: []string{"internal."}}
	c, _ := newCollector(t, config)
	config.Browser.Portals[0] = "attacker"
	config.Routes.ExcludedRoutes[0] = "changed"
	config.Routes.ExcludedPrefixes[0] = "changed"
	config.Routes.ExcludedOperations[0] = "changed"
	config.Routes.ExcludedOperationPrefixes[0] = "changed"
	routes := []http.RouteInfo{{ID: "example.read", Path: "/example/{id}"}}
	if err := c.SetRoutes(routes); err != nil {
		t.Fatal(err)
	}
	routes[0].Path = "/mutated"
	report := browserReport()
	if err := c.RecordBrowser(t.Context(), report); err != nil {
		t.Fatal(err)
	}
	report.Frames[0].File = "mutated"
	backend := backendReport()
	backend.Diagnostic.Frames = []fault.Frame{{File: "/private/server.go", Function: "example.read", Line: 42}}
	if err := c.Report(t.Context(), backend); err != nil {
		t.Fatal(err)
	}
	backend.Diagnostic.Frames[0].File = "mutated"
	c.ObserveRequest(t.Context(), request())
	b := capture(t, c)
	issues, buckets := b.issueSnapshot(), b.bucketSnapshot()
	issues[0].Detail.Frames[0].File = "changed"
	buckets[0].Metric.Buckets[0] = 999
	buckets[0].Path = "changed"
	again := b.issueSnapshot()
	if again[0].Detail.Frames[0].File != "admin/_assets/app.js" || again[1].Detail.Frames[0].File != "server.go" || b.bucketSnapshot()[0].Path != "/example/{id}" || b.bucketSnapshot()[0].Metric.Buckets[0] == 999 {
		t.Fatal("mutable caller/accessor data reached retained snapshot")
	}
	if c.config.Browser.Portals[0] != "admin" || !c.config.Routes.excludesRoute("excluded") || !c.config.Routes.excludesRoute("health.live") || !c.config.Routes.excludesOperation("skip") || !c.config.Routes.excludesOperation("internal.run") {
		t.Fatal("caller configuration was retained")
	}
}

func TestRouteInventoryIsOneTimeAndAtomic(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	if err := c.SetRoutes([]http.RouteInfo{{ID: "same", Path: "/one"}, {ID: "same", Path: "/two"}}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if len(c.paths) != 0 || c.routesSet {
		t.Fatal("failed registration partially installed")
	}
	if err := c.SetRoutes([]http.RouteInfo{{ID: "good", Path: "/good"}}); err != nil {
		t.Fatal(err)
	}
	if c.SetRoutes(nil) == nil {
		t.Fatal("second inventory accepted")
	}
	late, _ := newCollector(t, collectorConfig())
	late.ObserveRequest(t.Context(), request())
	if late.SetRoutes(nil) == nil {
		t.Fatal("inventory changed after admission")
	}
}

func TestBusinessBoundariesExclusionsAndRedaction(t *testing.T) {
	config := collectorConfig()
	config.Routes = RoutePolicy{ExcludedPrefixes: []string{"health."}, ExcludedOperations: []observability.Name{"jobs.skip"}, ExcludedOperationPrefixes: []string{"internal."}}
	c, _ := newCollector(t, config)
	for _, kind := range []observability.Kind{observability.Resource, observability.Provider, observability.CLI, observability.OutboundHTTP, observability.Socket} {
		r := backendReport()
		r.Entry.Operation.Kind = kind
		if err := c.Report(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []observability.Name{"telemetry.capture", "jobs.skip", "internal.work"} {
		r := backendReport()
		r.Entry.Operation.Name = name
		if err := c.Report(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	r := backendReport()
	r.Entry.Route.Name = "health.live"
	if err := c.Report(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	e := request()
	e.Route = "health.live"
	c.ObserveRequest(t.Context(), e)
	c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Request: e, Path: "/.env/secret-canary"})
	for _, kind := range []observability.Kind{observability.HTTP, observability.Job, observability.Schedule} {
		r := backendReport()
		r.Entry.Operation.Kind = kind
		r.Diagnostic.Faults = []fault.Note{{Message: "secret-canary"}}
		r.Diagnostic.Attributes = []fault.Attribute{{Key: "payload", Value: "secret-canary"}}
		r.Diagnostic.Frames = make([]fault.Frame, maxFrames+1)
		for i := range r.Diagnostic.Frames {
			r.Diagnostic.Frames[i] = fault.Frame{File: "/private-secret-canary/frame.go", Function: strings.Repeat("f", maxFunctionBytes+10), Line: 12}
		}
		if err := c.Report(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	b := capture(t, c)
	if len(b.issueSnapshot()) != 4 || len(b.bucketSnapshot()) != 2 {
		t.Fatal("business/exclusion/security boundary changed")
	}
	data, err := json.Marshal(b.issueSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-canary") {
		t.Fatal("private diagnostic data retained")
	}
	for _, issue := range b.issueSnapshot()[1:] {
		if len(issue.Detail.Frames) != maxFrames || len(issue.Detail.Frames[0].Function) != maxFunctionBytes {
			t.Fatal("backend frames unbounded")
		}
	}
}

func TestSecurityCategories(t *testing.T) {
	for _, test := range []struct{ path, code string }{{"/%zz", "malformed_path"}, {"/.ENV", "environment_probe"}, {"/.git/config", "repository_probe"}, {"/%2e%2e/secret", "traversal_probe"}, {"/..\\secret", "traversal_probe"}, {"/wp-login.php", "platform_probe"}} {
		t.Run(test.code+test.path, func(t *testing.T) {
			c, _ := newCollector(t, collectorConfig())
			c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Path: test.path})
			if got := capture(t, c).issueSnapshot()[0].Code; got != test.code {
				t.Fatalf("code = %q", got)
			}
		})
	}
	for _, status := range []int{401, 403, 429, 404, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, _ := newCollector(t, collectorConfig())
			c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Path: "/secret", Request: http.RequestEvent{Result: observability.Result{Status: status}}})
			b := capture(t, c)
			if status == 200 {
				if len(b.issueSnapshot()) != 0 {
					t.Fatal("ordinary request classified")
				}
				return
			}
			if len(b.issueSnapshot()) != 1 || b.issueSnapshot()[0].Detail.Path != "/[redacted]" || b.issueSnapshot()[0].Detail.ClientIP.IsSet() {
				t.Fatal("unsafe security detail")
			}
		})
	}
}

func TestConcurrentAdmissionAndCapture(t *testing.T) {
	config := collectorConfig()
	config.MaxEvents = 4096
	config.MaxPendingBatches = 60
	c, _ := newCollector(t, config)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 100 {
				c.ObserveRequest(t.Context(), request())
				if err := c.Report(t.Context(), backendReport()); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Go(func() {
		for range 20 {
			if err := c.capture(nil); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Wait()
	b := capture(t, c)
	for _, entry := range b.bucketSnapshot() {
		if entry.Metric.Count != 800 {
			t.Fatalf("count = %d", entry.Metric.Count)
		}
	}
	issues := 0
	ids := map[string]bool{}
	for _, pending := range c.pending {
		issues += len(pending.issueSnapshot())
		if ids[pending.id.String()] {
			t.Fatal("duplicate receipt identity")
		}
		ids[pending.id.String()] = true
	}
	if issues != 800 || c.dropped != 0 {
		t.Fatalf("issues/loss = %d/%d", issues, c.dropped)
	}
}
