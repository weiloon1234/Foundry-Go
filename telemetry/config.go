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
	// ExcludedRoutes lists route identities whose requests are excluded. Each entry must be a distinct
	// semantic identity.
	ExcludedRoutes []http.RouteID
	// ExcludedPrefixes excludes every route whose identity starts with one of these strings. Each entry must be a distinct
	// semantic identity (lowercase letters, digits, . _ -, starting with a letter or digit).
	ExcludedPrefixes []string
	// ExcludedOperations lists operation names whose backend issues are not collected. Each entry must be a distinct
	// semantic identity. An operation named telemetry or starting with telemetry. is always excluded.
	ExcludedOperations []observability.Name
	// ExcludedOperationPrefixes excludes every operation whose name starts with one of these strings. Each entry must be a
	// distinct semantic identity.
	ExcludedOperationPrefixes []string
}

// BrowserPolicy enables anonymous admission for an explicit portal allowlist.
type BrowserPolicy struct {
	// Enabled turns on browser error intake. While false, RecordBrowser refuses every report.
	Enabled bool
	// Portals is the allowlist of portals that may report. It must be non-empty when Enabled is true, and
	// each entry must be a distinct bounded identity.
	Portals []Portal
}

// CollectorConfig supplies identity and memory bounds. No zero-value defaults
// or alert thresholds are inferred. PendingRetention is one minute to one hour.
type CollectorConfig struct {
	// Instance names this process instance and is stored on its run. It must be 1 to 128 bytes of letters, digits
	// and . _ - characters, starting with a letter or digit and without "..".
	Instance Instance
	// Release names the deployed build. It is stored on the run and on every collected issue, and a
	// browser report whose release differs is refused. It follows the same identity rules as Instance.
	Release Release
	// MaxSeries bounds the in-memory request-metric series. It must be 1..4096.
	MaxSeries int
	// MaxEvents bounds the issue observations held before the next capture; observations over the bound
	// are counted as dropped. Browser issues may use at most MaxEvents/4 of it and security issues MaxEvents/2
	// (rounded down), which also caps the security issues one capture holds. It must be 1..4096.
	MaxEvents int
	// MaxPendingBatches bounds the captured batches waiting to be stored; when that many are pending a
	// new capture is dropped and counted. It must be 1..60.
	MaxPendingBatches int
	// PendingRetention is how long a captured batch may wait to be stored before it is discarded as
	// expired. It must be one minute to one hour.
	PendingRetention time.Duration
	// Routes excludes routes and operations from backend issue collection, and excluded routes from
	// alert evaluation and from traffic reads unless a read includes them.
	Routes RoutePolicy
	// Browser configures anonymous browser error intake.
	Browser BrowserPolicy
}

// Thresholds supplies explicit application choices. Ratios must be finite and in
// (0,1]; latency is in (0,60s], requests in 1..1,000,000 and auth failures in 1..4096.
type Thresholds struct {
	// Latency is the 95th-percentile request latency at or above which the latency rule is bad. It must
	// be above zero and at most one minute.
	Latency time.Duration
	// ErrorRatio is the share of requests answered with status 500 or above at or over which the HTTP
	// errors rule is bad. It must be finite and in (0,1].
	ErrorRatio float64
	// PoolUsageRatio is the database connection pool usage (open connections in use over the pool's
	// maximum, the higher of the primary and read pools) at or over which the pool pressure rule is bad.
	// It must be finite and in (0,1].
	PoolUsageRatio float64
	// MinimumRequests is the request count a capture needs before the latency and HTTP errors rules are
	// judged; below it those rules neither worsen nor recover. It must be 1..1,000,000.
	MinimumRequests int
	// AuthFailures is the number of authentication_failed and rate_limited security issues in one
	// capture at or over which the authentication burst rule is bad. It must be 1..4096, but a capture holds at most
	// CollectorConfig.MaxEvents/2 security issues, so a larger value never fires.
	AuthFailures int
}

// AlertPolicy selects built-in rules and capture-count sustain/recovery periods.
// Disabled policies may omit thresholds and counts entirely.
type AlertPolicy struct {
	// Enabled turns on incident evaluation as batches are stored. An enabled policy needs rules,
	// thresholds and both capture counts.
	Enabled bool
	// Rules lists the built-in rules to evaluate. Each must be a known rule, listed once.
	Rules []Rule
	// Thresholds supplies the limits the rules compare against. They are validated when Enabled is true
	// or any value is set.
	Thresholds Thresholds
	// SustainCaptures is how many consecutive bad captures open an incident for a rule, or reopen a recovered one as
	// a new episode. A capture below Thresholds.MinimumRequests restarts the count for the latency and HTTP errors
	// rules. It must be 1..60 when the policy is enabled or a count is set.
	SustainCaptures int
	// RecoverCaptures is how many consecutive healthy captures recover an open or acknowledged incident for a rule.
	// A capture below Thresholds.MinimumRequests restarts the count for the latency and HTTP errors rules. It must be
	// 1..60 when the policy is enabled or a count is set.
	RecoverCaptures int
}

// RetentionPolicy bounds future storage and pruning. Minute/Security are one
// hour to seven days; Hour/Issue are at most thirty days. Hour must cover Minute.
// Row ranges are runs 100..10,000, metrics 100..1,000,000, issues 100..10,000 and
// events 100..100,000. Receipt capacity and prune interval must be positive.
type RetentionPolicy struct {
	// Minute is how long minute-resolution metric buckets are kept. It must be one hour to seven days.
	Minute time.Duration
	// Hour is how long hour-resolution metric buckets and run records are kept. It must be at least
	// Minute and at most thirty days, and must exceed the receipt retry horizon.
	Hour time.Duration
	// Issue is how long backend and browser issues and their events are kept. It must be one hour to
	// thirty days.
	Issue time.Duration
	// Security is how long security issues and their events are kept. It must be one hour to seven days.
	Security time.Duration
	// MaxRuns bounds the stored process runs. At the bound, a new run is stored only if enough empty runs (quiet for
	// EmptyRunQuietPeriod) can be reclaimed; otherwise its batch is refused as overloaded. It must be 100..10,000.
	MaxRuns int
	// MaxReceiptRows bounds the stored flush receipts; at the bound a flush fails as overloaded. It must
	// be positive.
	MaxReceiptRows int
	// MaxMetricRows bounds the stored metric buckets; buckets over the bound are dropped and counted. It
	// must be 100..1,000,000.
	MaxMetricRows int
	// MaxIssueRows bounds the stored issue groups; groups over the bound are dropped and counted. It
	// must be 100..10,000.
	MaxIssueRows int
	// MaxEventRows bounds the stored issue events; events over the bound are dropped and counted. It
	// must be 100..100,000.
	MaxEventRows int
	// PruneInterval is the minimum time between pruning passes in the engine's loop. It must be positive.
	PruneInterval time.Duration
	// EmptyRunQuietPeriod defaults to one hour; explicit values are one hour to thirty days.
	EmptyRunQuietPeriod time.Duration
}

// ReadLimits contains explicit positive admission, result, page and timeout
// bounds. Config.Validate also compares result limits with storage capacities.
type ReadLimits struct {
	// MaxConcurrent bounds the reads in flight; another read fails as overloaded. It must be positive.
	MaxConcurrent int
	// MaxOverviewRuns bounds the runs an overview returns; more are truncated. It must be positive and
	// at most Retention.MaxRuns.
	MaxOverviewRuns int
	// MaxOverviewAlerts bounds the incidents an overview returns; more are truncated. It must be
	// positive and at most Retention.MaxRuns times the number of built-in rules.
	MaxOverviewAlerts int
	// MaxTrafficRows bounds the metric rows a traffic read scans; more are truncated. It must be
	// positive and at most Retention.MaxMetricRows.
	MaxTrafficRows int
	// MaxTrafficGroups bounds the distinct traffic groups a traffic read returns; more are truncated. It
	// must be positive and at most MaxTrafficRows.
	MaxTrafficGroups int
	// MaxPage is the highest page number a paged read accepts. It must be positive.
	MaxPage int
	// MaxPageSize is the largest page size a paged read accepts. It must be positive and at most
	// Retention.MaxIssueRows and Retention.MaxEventRows.
	MaxPageSize int
	// Timeout bounds one read; a read whose context ends before it completes fails with a timeout fault. It must be positive.
	Timeout time.Duration
}

// Config supplies future engine timing, retention, alert and read policy without
// starting an engine. FlushInterval is one second to one minute. Timeouts must be
// positive. Retry/receipt horizons additionally require the collector's retention
// at the eventual engine construction boundary; this value alone cannot check it.
type Config struct {
	// FlushInterval is how often the engine captures and stores a batch. It must be one second to one
	// minute.
	FlushInterval time.Duration
	// FlushTimeout bounds one storage or pruning attempt. It must be positive.
	FlushTimeout time.Duration
	// DrainTimeout bounds the final drain of pending batches at shutdown. It must be positive.
	DrainTimeout time.Duration
	// Retention bounds how long and how much telemetry is stored.
	Retention RetentionPolicy
	// Alerts configures the built-in incident rules, their thresholds and the capture counts that open and recover incidents. Evaluation runs only when Alerts.Enabled is true.
	Alerts AlertPolicy
	// Reads bounds concurrent reads, result sizes, page sizes and the read timeout; its result and page sizes must not exceed the matching storage capacities in Retention.
	Reads ReadLimits
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
	if p.Minute < time.Hour || p.Minute > maxMinuteRetention || p.Hour < p.Minute || p.Hour > maxRetention || p.Issue < time.Hour || p.Issue > maxRetention || p.Security < time.Hour || p.Security > maxMinuteRetention || p.MaxRuns < minStorageRows || p.MaxRuns > maxRuns || p.MaxReceiptRows < 1 || p.MaxMetricRows < minStorageRows || p.MaxMetricRows > maxMetricRows || p.MaxIssueRows < minStorageRows || p.MaxIssueRows > maxIssueRows || p.MaxEventRows < minStorageRows || p.MaxEventRows > maxEventRows || p.PruneInterval <= 0 || p.emptyRunQuietPeriod() < time.Hour || p.emptyRunQuietPeriod() > maxRetention {
		return invalid("invalid telemetry retention or storage bounds")
	}
	return nil
}

func (p RetentionPolicy) emptyRunQuietPeriod() time.Duration {
	if p.EmptyRunQuietPeriod == 0 {
		return time.Hour
	}
	return p.EmptyRunQuietPeriod
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
