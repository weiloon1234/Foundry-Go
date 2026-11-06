package telemetry

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type observedIssue struct {
	Source  Source
	Code    string
	Route   http.RouteID
	Release Release
	At      temporal.DateTime
	Detail  Detail
}

// Collector admits observations into bounded memory. It owns an independent
// RequestMetrics and never drains a recorder or cumulative Prometheus owner.
// Observation methods are concurrency-safe, do no storage I/O, and must return
// before request ownership ends. The injected clock must return promptly and
// support concurrent calls; clock panic/Goexit is contained by temporal.Now.
// Do not copy a Collector. Construct it with NewCollector.
type Collector struct {
	config CollectorConfig
	clock  clock.Clock
	// elapsed is the process-local monotonic clock, separate from application
	// timestamps. Only package tests replace it; consumers inject wall time above.
	elapsed func() time.Time
	metrics *http.RequestMetrics
	runID   model.ID[Run]
	started temporal.DateTime
	// Capture owns callbacks until exit, without holding the admission mutex.
	captureMu                   sync.Mutex
	mu                          sync.Mutex
	routesSet, admitted, sealed bool
	paths                       map[http.RouteID]string
	issues                      []observedIssue
	issueCounts                 map[Source]int
	buckets                     map[string]bucket
	pending                     []batch
	dropped, failures           uint64
	storageOwned                bool
	activeBatch                 model.ID[batch]
	uncertainBatch              model.ID[batch]
}

var _ http.SecurityRequestObserver = (*Collector)(nil)

// NewCollector validates and snapshots configuration and reads the explicit
// clock to create a UUIDv7 run identity. It starts no background runtime owner.
// A nil clock is invalid; pass clock.System{} for ordinary application time.
func NewCollector(c CollectorConfig, source clock.Clock) (*Collector, error) {
	c = c.clone()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	now, err := temporal.Now(source)
	if err != nil {
		return nil, err
	}
	id, err := model.NewIDAt[Run](now.UTC())
	if err != nil {
		return nil, err
	}
	metrics, err := http.NewRequestMetrics(c.MaxSeries)
	if err != nil {
		return nil, err
	}
	return &Collector{config: c, clock: source, elapsed: time.Now, metrics: metrics, runID: id, started: now, paths: make(map[http.RouteID]string), issueCounts: make(map[Source]int), buckets: make(map[string]bucket)}, nil
}

// RunID returns this collector's stable, model-owned process identity.
func (c *Collector) RunID() model.ID[Run] { return c.runID }

// SetRoutes snapshots declared route templates once before any observation or
// capture. Duplicate IDs fail atomically. Without an inventory, metric paths
// remain empty; observations still use validated declared route identities.
func (c *Collector) SetRoutes(routes []http.RouteInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.routesSet || c.admitted || c.sealed {
		return safeerror.NewFault(fault.Conflict, "telemetry route registration is closed")
	}
	paths := make(map[http.RouteID]string, len(routes))
	for _, route := range routes {
		if !identifier.Semantic(string(route.ID)) || !strings.HasPrefix(route.Path, "/") || strings.ContainsAny(route.Path, "?\x00\r\n\t#") {
			return invalid("invalid telemetry route metadata")
		}
		if _, exists := paths[route.ID]; exists {
			return invalid("duplicate telemetry route")
		}
		paths[route.ID] = strings.Clone(route.Path)
	}
	c.paths, c.routesSet = paths, true
	return nil
}

// ObserveRequest records traffic, including excluded routes. Exclusions apply
// to later alert/read selection, while security classification is independent.
func (c *Collector) ObserveRequest(ctx context.Context, e http.RequestEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.admitLocked() {
		return
	}
	if e.Route != "" && !identifier.Semantic(string(e.Route)) {
		e.Route = ""
	}
	if _, err := e.Result.Normalized(observability.HTTP); err != nil {
		c.dropLocked(1)
		return
	}
	c.metrics.ObserveRequest(ctx, e)
}

// ObserveSecurityRequest classifies before discarding the raw path. Only a
// structural category, declared route, correlation ID and trusted-proxy address
// survive; paths, queries and credentials never enter retained detail.
func (c *Collector) ObserveSecurityRequest(_ context.Context, e http.SecurityRequestEvent) {
	if !c.beginObservation() {
		return
	}
	code, probe := classifySecurityPath(e.Path)
	redacted := "/[redacted]"
	if probe {
		redacted = "/[" + code + "]"
	} else {
		switch e.Request.Result.Status {
		case 401:
			code = "authentication_failed"
		case 403:
			code = "permission_denied"
		case 429:
			code = "rate_limited"
		case 404:
			code = "unmatched_path"
		default:
			return
		}
	}
	now, err := temporal.Now(c.clock)
	if err != nil {
		c.drop(1)
		return
	}
	route := e.Request.Route
	if route != "" && !identifier.Semantic(string(route)) {
		route = ""
	}
	detail := Detail{Path: redacted, RequestID: attribution.RequestID(safeIdentifier(string(e.Request.RequestID), attribution.MaxRequestIDBytes))}
	if e.ClientIP.IsValid() {
		detail.ClientIP = value.Set(e.ClientIP.WithZone(""))
	}
	c.add(observedIssue{Source: SourceSecurity, Code: code, Route: route, Release: c.config.Release, At: now, Detail: detail})
}

// Report accepts safe recorder diagnostics at HTTP/job/schedule business
// boundaries. Resource/provider reports, app exclusions and telemetry's own
// operations are ignored to prevent recursive reporting. Fault messages and
// attributes are not retained. Queue-full admission counts loss and returns nil.
func (c *Collector) Report(_ context.Context, r observability.ErrorReport) error {
	if !c.beginObservation() {
		return nil
	}
	switch r.Entry.Operation.Kind {
	case observability.HTTP, observability.Job, observability.Schedule:
	default:
		return nil
	}
	if c.config.Routes.excludesRoute(http.RouteID(r.Entry.Route.Name)) || c.config.Routes.excludesOperation(r.Entry.Operation.Name) {
		return nil
	}
	if err := r.Entry.Operation.Validate(); err != nil {
		c.drop(1)
		return err
	}
	code := string(r.Entry.Operation.Name)
	if len(r.Diagnostic.Types) > 0 {
		code = safeIdentifier(r.Diagnostic.Types[0], maxIdentityBytes)
	}
	detail := Detail{RequestID: attribution.RequestID(safeIdentifier(string(r.Entry.RequestID), attribution.MaxRequestIDBytes)), TraceID: r.Entry.TraceID}
	for _, frame := range r.Diagnostic.Frames[:min(maxFrames, len(r.Diagnostic.Frames))] {
		file := frame.File[strings.LastIndexAny(frame.File, "/\\")+1:]
		detail.Frames = append(detail.Frames, Frame{File: safeIdentifier(file, maxIdentityBytes), Function: safeIdentifier(frame.Function, maxFunctionBytes), Line: max(0, frame.Line)})
	}
	now, err := temporal.Now(c.clock)
	if err != nil {
		c.drop(1)
		return err
	}
	route := http.RouteID(r.Entry.Route.Name)
	if route != "" && !identifier.Semantic(string(route)) {
		route = ""
	}
	c.add(observedIssue{Source: SourceBackend, Code: code, Route: route, Release: c.config.Release, At: now, Detail: detail})
	return nil
}

func (c *Collector) beginObservation() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.admitLocked()
}

func (c *Collector) admitLocked() bool {
	c.admitted = true
	if c.sealed {
		c.dropLocked(1)
		return false
	}
	return true
}

func (c *Collector) add(issue observedIssue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sealed {
		c.dropLocked(1)
		return
	}
	limit := c.config.MaxEvents
	switch issue.Source {
	case SourceBrowser:
		limit /= 4
	case SourceSecurity:
		limit /= 2
	}
	if c.issueCounts[issue.Source] >= limit || len(c.issues) >= c.config.MaxEvents {
		c.dropLocked(1)
		return
	}
	c.issues = append(c.issues, issue)
	c.issueCounts[issue.Source]++
}

func (c *Collector) drop(n uint64)       { c.mu.Lock(); defer c.mu.Unlock(); c.dropLocked(n) }
func (c *Collector) dropLocked(n uint64) { c.dropped = saturatingAdd(c.dropped, n) }

func saturatingAdd(a, b uint64) uint64 {
	if ^uint64(0)-a < b {
		return ^uint64(0)
	}
	return a + b
}

// seal is the future engine's admission barrier; it does not drain or persist.
func (c *Collector) seal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sealed = true
}
