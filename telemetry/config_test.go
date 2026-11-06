package telemetry

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func engineConfig() Config {
	return Config{FlushInterval: 10 * time.Second, FlushTimeout: 5 * time.Second, DrainTimeout: 3 * time.Second,
		Retention: RetentionPolicy{Minute: 7 * 24 * time.Hour, Hour: 30 * 24 * time.Hour, Issue: 30 * 24 * time.Hour, Security: 7 * 24 * time.Hour, MaxRuns: 1000, MaxReceiptRows: 100000, MaxMetricRows: 1000000, MaxIssueRows: 10000, MaxEventRows: 100000, PruneInterval: time.Minute},
		Reads:     ReadLimits{MaxConcurrent: 4, MaxOverviewRuns: 100, MaxOverviewAlerts: 100, MaxTrafficRows: 1000000, MaxTrafficGroups: 4096, MaxPage: 100, MaxPageSize: 30, Timeout: time.Second}}
}

func alertPolicy() AlertPolicy {
	return AlertPolicy{Enabled: true, Rules: []Rule{RuleLatency, RuleHTTPErrors, RulePoolPressure, RuleLogFailures, RuleTelemetryLoss, RuleAuthenticationBurst}, Thresholds: Thresholds{Latency: time.Second, ErrorRatio: .05, PoolUsageRatio: .9, MinimumRequests: 100, AuthFailures: 20}, SustainCaptures: 3, RecoverCaptures: 2}
}

func TestCollectorConfigRanges(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*CollectorConfig)
	}{
		{"instance empty", func(c *CollectorConfig) { c.Instance = "" }},
		{"instance traversal", func(c *CollectorConfig) { c.Instance = "a..b" }},
		{"instance slash", func(c *CollectorConfig) { c.Instance = "a/b" }},
		{"release long", func(c *CollectorConfig) { c.Release = Release(strings.Repeat("r", 129)) }},
		{"series zero", func(c *CollectorConfig) { c.MaxSeries = 0 }},
		{"series high", func(c *CollectorConfig) { c.MaxSeries = 4097 }},
		{"events zero", func(c *CollectorConfig) { c.MaxEvents = 0 }},
		{"events high", func(c *CollectorConfig) { c.MaxEvents = 4097 }},
		{"pending zero", func(c *CollectorConfig) { c.MaxPendingBatches = 0 }},
		{"pending high", func(c *CollectorConfig) { c.MaxPendingBatches = 61 }},
		{"retention low", func(c *CollectorConfig) { c.PendingRetention = time.Minute - time.Nanosecond }},
		{"retention high", func(c *CollectorConfig) { c.PendingRetention = time.Hour + time.Nanosecond }},
		{"duplicate routes", func(c *CollectorConfig) { c.Routes.ExcludedRoutes = []http.RouteID{"same", "same"} }},
		{"duplicate prefixes", func(c *CollectorConfig) { c.Routes.ExcludedPrefixes = []string{"same.", "same."} }},
		{"duplicate operations", func(c *CollectorConfig) { c.Routes.ExcludedOperations = []observability.Name{"same", "same"} }},
		{"duplicate operation prefixes", func(c *CollectorConfig) { c.Routes.ExcludedOperationPrefixes = []string{"same.", "same."} }},
		{"path exclusion", func(c *CollectorConfig) { c.Routes.ExcludedPrefixes = []string{"/private"} }},
		{"empty exclusion", func(c *CollectorConfig) { c.Routes.ExcludedPrefixes = []string{""} }},
		{"empty portals", func(c *CollectorConfig) { c.Browser.Portals = nil }},
		{"duplicate portals", func(c *CollectorConfig) { c.Browser.Portals = []Portal{"admin", "admin"} }},
		{"invalid portal", func(c *CollectorConfig) { c.Browser.Portals = []Portal{"../admin"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := collectorConfig()
			test.edit(&c)
			if err := c.Validate(); err == nil || !errors.Is(err, fault.Invalid) {
				t.Fatalf("validation = %v", err)
			}
		})
	}
	for _, high := range []bool{false, true} {
		c := collectorConfig()
		c.MaxSeries = 1
		c.MaxEvents = 1
		c.MaxPendingBatches = 1
		c.PendingRetention = time.Minute
		if high {
			c.MaxSeries = 4096
			c.MaxEvents = 4096
			c.MaxPendingBatches = 60
			c.PendingRetention = time.Hour
			c.Instance = Instance(strings.Repeat("x", 128))
			c.Release = Release(strings.Repeat("x", 128))
		}
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConfigRetentionReadAndTimingBounds(t *testing.T) {
	if err := engineConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*Config)
	}{
		{"flush low", func(c *Config) { c.FlushInterval = time.Second - time.Nanosecond }},
		{"flush high", func(c *Config) { c.FlushInterval = time.Minute + time.Nanosecond }},
		{"flush timeout", func(c *Config) { c.FlushTimeout = 0 }},
		{"drain timeout", func(c *Config) { c.DrainTimeout = 0 }},
		{"minute low", func(c *Config) { c.Retention.Minute = time.Hour - time.Nanosecond }},
		{"minute high", func(c *Config) { c.Retention.Minute = 7*24*time.Hour + time.Nanosecond }},
		{"hour shorter", func(c *Config) { c.Retention.Hour = c.Retention.Minute - time.Nanosecond }},
		{"hour high", func(c *Config) { c.Retention.Hour = 30*24*time.Hour + time.Nanosecond }},
		{"issue low", func(c *Config) { c.Retention.Issue = time.Hour - time.Nanosecond }},
		{"issue high", func(c *Config) { c.Retention.Issue = 30*24*time.Hour + time.Nanosecond }},
		{"security low", func(c *Config) { c.Retention.Security = time.Hour - time.Nanosecond }},
		{"security high", func(c *Config) { c.Retention.Security = 7*24*time.Hour + time.Nanosecond }},
		{"runs low", func(c *Config) { c.Retention.MaxRuns = 99 }},
		{"runs high", func(c *Config) { c.Retention.MaxRuns = 10001 }},
		{"receipt capacity", func(c *Config) { c.Retention.MaxReceiptRows = 0 }},
		{"metrics low", func(c *Config) { c.Retention.MaxMetricRows = 99 }},
		{"metrics high", func(c *Config) { c.Retention.MaxMetricRows = 1000001 }},
		{"issues low", func(c *Config) { c.Retention.MaxIssueRows = 99 }},
		{"issues high", func(c *Config) { c.Retention.MaxIssueRows = 10001 }},
		{"events low", func(c *Config) { c.Retention.MaxEventRows = 99 }},
		{"events high", func(c *Config) { c.Retention.MaxEventRows = 100001 }},
		{"prune interval", func(c *Config) { c.Retention.PruneInterval = 0 }},
		{"read concurrency", func(c *Config) { c.Reads.MaxConcurrent = 0 }},
		{"read runs", func(c *Config) { c.Reads.MaxOverviewRuns = 0 }},
		{"read alerts", func(c *Config) { c.Reads.MaxOverviewAlerts = 0 }},
		{"read rows", func(c *Config) { c.Reads.MaxTrafficRows = 0 }},
		{"read groups", func(c *Config) { c.Reads.MaxTrafficGroups = 0 }},
		{"read page", func(c *Config) { c.Reads.MaxPage = 0 }},
		{"read page size", func(c *Config) { c.Reads.MaxPageSize = 0 }},
		{"read timeout", func(c *Config) { c.Reads.Timeout = 0 }},
		{"page overflow", func(c *Config) { c.Reads.MaxPage = int(^uint(0) >> 1) }},
		{"run cap", func(c *Config) { c.Reads.MaxOverviewRuns = c.Retention.MaxRuns + 1 }},
		{"alert cap", func(c *Config) { c.Reads.MaxOverviewAlerts = c.Retention.MaxRuns*6 + 1 }},
		{"traffic cap", func(c *Config) { c.Reads.MaxTrafficRows = c.Retention.MaxMetricRows + 1 }},
		{"group cap", func(c *Config) { c.Reads.MaxTrafficGroups = c.Reads.MaxTrafficRows + 1 }},
		{"issue page cap", func(c *Config) { c.Reads.MaxPageSize = c.Retention.MaxIssueRows + 1 }},
		{"event page cap", func(c *Config) { c.Retention.MaxEventRows = 100; c.Reads.MaxPageSize = 101 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := engineConfig()
			test.edit(&c)
			if c.Validate() == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	for _, high := range []bool{false, true} {
		p := engineConfig().Retention
		p.Minute = time.Hour
		p.Hour = time.Hour
		p.Issue = time.Hour
		p.Security = time.Hour
		p.MaxRuns = 100
		p.MaxMetricRows = 100
		p.MaxIssueRows = 100
		p.MaxEventRows = 100
		p.MaxReceiptRows = 1
		if high {
			p = engineConfig().Retention
			p.MaxRuns = 10000
		}
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAlertPolicyHasNoImplicitThresholds(t *testing.T) {
	if err := (AlertPolicy{}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := alertPolicy().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*AlertPolicy)
	}{
		{"missing rules", func(p *AlertPolicy) { p.Rules = nil }},
		{"duplicate rule", func(p *AlertPolicy) { p.Rules = []Rule{RuleLatency, RuleLatency} }},
		{"unknown rule", func(p *AlertPolicy) { p.Rules = []Rule{"custom"} }},
		{"missing thresholds", func(p *AlertPolicy) { p.Thresholds = Thresholds{} }},
		{"latency zero", func(p *AlertPolicy) { p.Thresholds.Latency = 0 }},
		{"latency high", func(p *AlertPolicy) { p.Thresholds.Latency = time.Minute + 1 }},
		{"ratio zero", func(p *AlertPolicy) { p.Thresholds.ErrorRatio = 0 }},
		{"ratio high", func(p *AlertPolicy) { p.Thresholds.ErrorRatio = 1.01 }},
		{"ratio nan", func(p *AlertPolicy) { p.Thresholds.ErrorRatio = math.NaN() }},
		{"pool zero", func(p *AlertPolicy) { p.Thresholds.PoolUsageRatio = 0 }},
		{"pool infinite", func(p *AlertPolicy) { p.Thresholds.PoolUsageRatio = math.Inf(1) }},
		{"requests zero", func(p *AlertPolicy) { p.Thresholds.MinimumRequests = 0 }},
		{"requests high", func(p *AlertPolicy) { p.Thresholds.MinimumRequests = 1000001 }},
		{"auth zero", func(p *AlertPolicy) { p.Thresholds.AuthFailures = 0 }},
		{"auth high", func(p *AlertPolicy) { p.Thresholds.AuthFailures = 4097 }},
		{"sustain zero", func(p *AlertPolicy) { p.SustainCaptures = 0 }},
		{"sustain high", func(p *AlertPolicy) { p.SustainCaptures = 61 }},
		{"recover zero", func(p *AlertPolicy) { p.RecoverCaptures = 0 }},
		{"recover high", func(p *AlertPolicy) { p.RecoverCaptures = 61 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := alertPolicy()
			test.edit(&p)
			if p.Validate() == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	p := alertPolicy()
	p.Thresholds = Thresholds{Latency: time.Minute, ErrorRatio: 1, PoolUsageRatio: 1, MinimumRequests: 1000000, AuthFailures: 4096}
	p.SustainCaptures = 60
	p.RecoverCaptures = 60
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityParsingAndFingerprintGrouping(t *testing.T) {
	text := strings.Repeat("ab", 32)
	if got, err := ParseFingerprint(text); err != nil || string(got) != text {
		t.Fatal("fingerprint rejected", err)
	}
	if got, err := ParseAlertKey(text); err != nil || string(got) != text {
		t.Fatal("alert key rejected", err)
	}
	for _, bad := range []string{"", strings.ToUpper(text), text[:63], text + "0", strings.Repeat("g", 64)} {
		if _, err := ParseFingerprint(bad); err == nil {
			t.Fatal("bad fingerprint accepted")
		}
		if _, err := ParseAlertKey(bad); err == nil {
			t.Fatal("bad alert key accepted")
		}
	}
	a := observedIssue{Source: SourceBrowser, Code: "TypeError", Route: "browser.admin", Release: "development"}
	b := a
	b.Detail.Frames = []Frame{{File: "different.js", Line: 42}}
	if fingerprint(a) != fingerprint(b) {
		t.Fatal("frames changed grouping")
	}
	b.Release = "next"
	if fingerprint(a) == fingerprint(b) {
		t.Fatal("release omitted from grouping")
	}
}
