package telemetry

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// RoutePolicy excludes backend issues and identifies traffic excluded from later
// alert/read aggregation. HTTP metrics and security classification still run.
// Prefixes match declared identities, never raw request paths.
type RoutePolicy struct {
	ExcludedRoutes            []http.RouteID
	ExcludedPrefixes          []string
	ExcludedOperations        []observability.Name
	ExcludedOperationPrefixes []string
}

// BrowserPolicy enables anonymous admission for an explicit portal allowlist.
type BrowserPolicy struct {
	Enabled bool
	Portals []Portal
}

// CollectorConfig supplies identity and memory bounds. No zero-value defaults
// or alert thresholds are inferred. PendingRetention is one minute to one hour.
type CollectorConfig struct {
	Instance                                Instance
	Release                                 Release
	MaxSeries, MaxEvents, MaxPendingBatches int
	PendingRetention                        time.Duration
	Routes                                  RoutePolicy
	Browser                                 BrowserPolicy
}

// Thresholds supplies explicit application choices. Ratios must be finite and in
// (0,1]; latency is in (0,60s], requests in 1..1,000,000 and auth failures in 1..4096.
type Thresholds struct {
	Latency                       time.Duration
	ErrorRatio, PoolUsageRatio    float64
	MinimumRequests, AuthFailures int
}

// AlertPolicy selects built-in rules and capture-count sustain/recovery periods.
// Disabled policies may omit thresholds and counts entirely.
type AlertPolicy struct {
	Enabled                          bool
	Rules                            []Rule
	Thresholds                       Thresholds
	SustainCaptures, RecoverCaptures int
}

// RetentionPolicy bounds future storage and pruning. Minute/Security are one
// hour to seven days; Hour/Issue are at most thirty days. Hour must cover Minute.
// Row ranges are runs 100..10,000, metrics 100..1,000,000, issues 100..10,000 and
// events 100..100,000. Receipt capacity and prune interval must be positive.
type RetentionPolicy struct {
	Minute, Hour, Issue, Security                                      time.Duration
	MaxRuns, MaxReceiptRows, MaxMetricRows, MaxIssueRows, MaxEventRows int
	PruneInterval                                                      time.Duration
}

// ReadLimits contains explicit positive admission, result, page and timeout
// bounds. Config.Validate also compares result limits with storage capacities.
type ReadLimits struct {
	MaxConcurrent, MaxOverviewRuns, MaxOverviewAlerts      int
	MaxTrafficRows, MaxTrafficGroups, MaxPage, MaxPageSize int
	Timeout                                                time.Duration
}

// Config supplies future engine timing, retention, alert and read policy without
// starting an engine. FlushInterval is one second to one minute. Timeouts must be
// positive. Retry/receipt horizons additionally require the collector's retention
// at the eventual engine construction boundary; this value alone cannot check it.
type Config struct {
	FlushInterval, FlushTimeout, DrainTimeout time.Duration
	Retention                                 RetentionPolicy
	Alerts                                    AlertPolicy
	Reads                                     ReadLimits
}

func invalid(message string) error { return safeerror.NewFault(fault.Invalid, message) }

// Validate checks distinct bounded route/operation exclusions.
func (p RoutePolicy) Validate() error {
	if !distinctNames(p.ExcludedRoutes) || !distinctNames(p.ExcludedPrefixes) || !distinctNames(p.ExcludedOperations) || !distinctNames(p.ExcludedOperationPrefixes) {
		return invalid("telemetry exclusions require distinct semantic identities")
	}
	return nil
}

func distinctNames[T ~string](names []T) bool {
	seen := make(map[T]bool, len(names))
	for _, name := range names {
		if !identifier.Semantic(string(name)) || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

// Validate checks the explicit browser allowlist, including duplicate portals.
func (p BrowserPolicy) Validate() error {
	if p.Enabled && len(p.Portals) == 0 {
		return invalid("enabled browser telemetry requires portals")
	}
	seen := make(map[Portal]bool, len(p.Portals))
	for _, portal := range p.Portals {
		if !validIdentity(string(portal)) || seen[portal] {
			return invalid("telemetry portals require distinct bounded identities")
		}
		seen[portal] = true
	}
	return nil
}

// Validate checks identity, nested policies and Starter-compatible memory ranges:
// series/events 1..4096, pending batches 1..60, retention one minute to one hour.
func (c CollectorConfig) Validate() error {
	if !validIdentity(string(c.Instance)) || !validIdentity(string(c.Release)) || c.MaxSeries < 1 || c.MaxSeries > maxSeries || c.MaxEvents < 1 || c.MaxEvents > maxEvents || c.MaxPendingBatches < 1 || c.MaxPendingBatches > maxPendingBatches || c.PendingRetention < time.Minute || c.PendingRetention > time.Hour {
		return invalid("invalid telemetry collector identity or bounds")
	}
	if err := c.Routes.Validate(); err != nil {
		return err
	}
	return c.Browser.Validate()
}

// Validate checks every threshold without choosing application defaults.
func (t Thresholds) Validate() error {
	if t.Latency <= 0 || t.Latency > time.Minute || !validRatio(t.ErrorRatio) || !validRatio(t.PoolUsageRatio) || t.MinimumRequests < 1 || t.MinimumRequests > maxMinimumRequests || t.AuthFailures < 1 || t.AuthFailures > maxEvents {
		return invalid("invalid telemetry alert thresholds")
	}
	return nil
}

func validRatio(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) && n > 0 && n <= 1 }

// Validate checks rule identity/uniqueness and explicit thresholds/counts when
// enabled or supplied. A disabled zero policy is valid and supplies no defaults.
func (p AlertPolicy) Validate() error {
	seen := make(map[Rule]bool, len(p.Rules))
	for _, rule := range p.Rules {
		if !rule.IsValid() {
			return invalid("unknown telemetry alert rule")
		}
		if seen[rule] {
			return invalid("duplicate telemetry alert rule")
		}
		seen[rule] = true
	}
	if p.Enabled && len(p.Rules) == 0 {
		return invalid("enabled telemetry alerts require rules")
	}
	if p.Enabled || p.Thresholds != (Thresholds{}) {
		if err := p.Thresholds.Validate(); err != nil {
			return err
		}
	}
	if p.Enabled || p.SustainCaptures != 0 || p.RecoverCaptures != 0 {
		if p.SustainCaptures < 1 || p.SustainCaptures > maxCaptures || p.RecoverCaptures < 1 || p.RecoverCaptures > maxCaptures {
			return invalid("invalid telemetry capture counts")
		}
	}
	return nil
}

// Validate checks retention durations, row capacities and prune timing.
func (p RetentionPolicy) Validate() error {
	if p.Minute < time.Hour || p.Minute > maxMinuteRetention || p.Hour < p.Minute || p.Hour > maxRetention || p.Issue < time.Hour || p.Issue > maxRetention || p.Security < time.Hour || p.Security > maxMinuteRetention || p.MaxRuns < minStorageRows || p.MaxRuns > maxRuns || p.MaxReceiptRows < 1 || p.MaxMetricRows < minStorageRows || p.MaxMetricRows > maxMetricRows || p.MaxIssueRows < minStorageRows || p.MaxIssueRows > maxIssueRows || p.MaxEventRows < minStorageRows || p.MaxEventRows > maxEventRows || p.PruneInterval <= 0 {
		return invalid("invalid telemetry retention or storage bounds")
	}
	return nil
}

// Validate requires positive read bounds and a representable page offset.
func (l ReadLimits) Validate() error {
	if l.MaxConcurrent < 1 || l.MaxOverviewRuns < 1 || l.MaxOverviewAlerts < 1 || l.MaxTrafficRows < 1 || l.MaxTrafficGroups < 1 || l.MaxPage < 1 || l.MaxPageSize < 1 || l.Timeout <= 0 {
		return invalid("invalid telemetry read bounds")
	}
	if l.MaxPage > int(^uint(0)>>1)/l.MaxPageSize {
		return invalid("telemetry page bounds overflow")
	}
	return nil
}

// Validate checks timing, nested policies, and read caps against storage caps.
func (c Config) Validate() error {
	if c.FlushInterval < time.Second || c.FlushInterval > time.Minute || c.FlushTimeout <= 0 || c.DrainTimeout <= 0 {
		return invalid("invalid telemetry timing")
	}
	if err := c.Retention.Validate(); err != nil {
		return err
	}
	if err := c.Alerts.Validate(); err != nil {
		return err
	}
	if err := c.Reads.Validate(); err != nil {
		return err
	}
	// One incident per built-in rule per retained run is the incident capacity.
	if c.Reads.MaxOverviewRuns > c.Retention.MaxRuns || c.Reads.MaxOverviewAlerts > c.Retention.MaxRuns*len(RuleValues()) || c.Reads.MaxTrafficRows > c.Retention.MaxMetricRows || c.Reads.MaxTrafficGroups > c.Reads.MaxTrafficRows || c.Reads.MaxPageSize > c.Retention.MaxIssueRows || c.Reads.MaxPageSize > c.Retention.MaxEventRows {
		return invalid("telemetry read limits exceed storage capacities")
	}
	return nil
}

func (p RoutePolicy) excludesRoute(route http.RouteID) bool {
	if slices.Contains(p.ExcludedRoutes, route) {
		return true
	}
	for _, prefix := range p.ExcludedPrefixes {
		if strings.HasPrefix(string(route), prefix) {
			return true
		}
	}
	return false
}

func (p RoutePolicy) excludesOperation(name observability.Name) bool {
	if name == "telemetry" || strings.HasPrefix(string(name), "telemetry.") || slices.Contains(p.ExcludedOperations, name) {
		return true
	}
	for _, prefix := range p.ExcludedOperationPrefixes {
		if strings.HasPrefix(string(name), prefix) {
			return true
		}
	}
	return false
}

func (c CollectorConfig) clone() CollectorConfig {
	c.Routes.ExcludedRoutes = slices.Clone(c.Routes.ExcludedRoutes)
	c.Routes.ExcludedPrefixes = slices.Clone(c.Routes.ExcludedPrefixes)
	c.Routes.ExcludedOperations = slices.Clone(c.Routes.ExcludedOperations)
	c.Routes.ExcludedOperationPrefixes = slices.Clone(c.Routes.ExcludedOperationPrefixes)
	c.Browser.Portals = slices.Clone(c.Browser.Portals)
	return c
}
