// Package observability owns bounded operation observations and pluggable error
// reporting shared by framework kernels. It starts no independent HTTP server.
package observability

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

type Kind string

const (
	HTTP          Kind = "http"
	OutboundHTTP  Kind = "outbound_http"
	Job           Kind = "job"
	Schedule      Kind = "schedule"
	Socket        Kind = "socket"
	SocketMessage Kind = "socket_message"
	CLI           Kind = "cli"
	Provider      Kind = "provider"
	Kernel        Kind = "kernel"
	Resource      Kind = "resource"
)

type Name string
type Operation struct {
	Kind Kind `json:"kind"`
	Name Name `json:"name"`
}

func (o Operation) Validate() error {
	switch o.Kind {
	case HTTP, OutboundHTTP, Job, Schedule, Socket, SocketMessage, CLI, Provider, Kernel, Resource:
	default:
		return fault.New(fault.Invalid, "invalid observed operation kind")
	}
	if len(o.Name) > 128 || !identifier.Semantic(string(o.Name)) {
		return fault.New(fault.Invalid, "observed operation requires a bounded declared name")
	}
	return nil
}

type Outcome string

const (
	Succeeded Outcome = "succeeded"
	Failed    Outcome = "failed"
	Rejected  Outcome = "rejected"
	Cancelled Outcome = "cancelled"
	TimedOut  Outcome = "timed_out"
	Panicked  Outcome = "panicked"
)

// Result contains operational metadata only. Status is an optional HTTP status;
// zero Outcome derives success/rejection/failure from it. Never put error text,
// URLs, payloads or user identifiers into an operation's declared metric name.
type Result struct {
	Outcome Outcome `json:"outcome"`
	Status  int     `json:"status,omitempty"`
}

// Normalized validates a result and derives an omitted outcome from status.
// Transport observers use the same classification as the shared recorder.
func (r Result) Normalized(kind Kind) (Result, error) { return r.normalize(kind) }

func (r Result) normalize(kind Kind) (Result, error) {
	if r.Status != 0 && (kind != HTTP || r.Status < 100 || r.Status > 599) {
		return Result{}, fault.New(fault.Invalid, "invalid observation status")
	}
	if r.Outcome == "" {
		r.Outcome = Succeeded
		if r.Status >= 500 {
			r.Outcome = Failed
		} else if r.Status >= 400 {
			r.Outcome = Rejected
		}
	}
	switch r.Outcome {
	case Succeeded, Failed, Rejected, Cancelled, TimedOut, Panicked:
		return r, nil
	default:
		return Result{}, fault.New(fault.Invalid, "invalid observation outcome")
	}
}

// Entry is a bounded, payload-free operation record. Correlation IDs belong in
// recent observations and reports, never metric labels. Vendor tracestate is
// deliberately absent; raw request paths, IPs, identities and error chains are
// not retained by the recorder.
type Entry struct {
	Operation Operation             `json:"operation"`
	Result    Result                `json:"result"`
	Started   time.Time             `json:"started"`
	Duration  time.Duration         `json:"duration_ns"`
	TraceID   tracing.TraceID       `json:"trace_id"`
	SpanID    tracing.SpanID        `json:"span_id"`
	ParentID  tracing.SpanID        `json:"parent_id"`
	RequestID attribution.RequestID `json:"request_id,omitempty"`
	Route     attribution.Route     `json:"route,omitzero"`
}

// ErrorReport describes one failed operation. Diagnostic is a redacted summary
// (type names, framework fault notes and attributes, contained-panic frames);
// it never contains error text, payloads or credentials.
type ErrorReport struct {
	Entry      Entry            `json:"entry"`
	Diagnostic fault.Diagnostic `json:"diagnostic,omitzero"`
}

// Reporter receives owned metadata in a bounded worker. It must honor context
// cancellation and return only after its work ends. Recorder shutdown retains
// ownership of callbacks that ignore the deadline. Panics/Goexit are isolated.
type Reporter func(context.Context, ErrorReport) error

// TraceExporter receives sampled completed spans without payloads or vendor
// state. Exporters use their own bounded queue so trace traffic cannot evict
// error reports. Each callback must own its I/O until it returns.
type TraceExporter func(context.Context, Entry) error

// TraceBatchExporter receives up to Config.TraceBatchSize sampled spans that
// were already queued together, in queue order. A trace worker never waits to
// fill a batch, so light traffic exports promptly in small batches. The slice
// is owned by the callback. One failed call counts one export failure.
type TraceBatchExporter func(context.Context, []Entry) error

// DefaultTraceBatchSize bounds one TraceBatchExporter call when unset.
const DefaultTraceBatchSize = 64

type Config struct {
	MaxSeries           int
	MaxRecent           int
	MaxActive           int
	ErrorQueue          int
	ReporterConcurrency int
	ReporterTimeout     time.Duration
	// SampleTraces enables local trace sampling. TraceSampleRatio then selects
	// the sampled fraction of traces, deterministically by trace ID; zero
	// selects 1 (every trace). Disable sampling with SampleTraces=false.
	SampleTraces        bool
	TraceSampleRatio    float64
	TraceExporters      []TraceExporter
	TraceBatchExporters []TraceBatchExporter
	// TraceBatchSize bounds one batch exporter call; zero selects 64.
	TraceBatchSize   int
	TraceQueue       int
	TraceConcurrency int
	TraceTimeout     time.Duration
	Maintenance      *maintenance.Gate
}

func DefaultConfig() Config {
	return Config{MaxSeries: 512, MaxRecent: 128, MaxActive: 65536, ErrorQueue: 128, ReporterConcurrency: 2, ReporterTimeout: 3 * time.Second, SampleTraces: true, TraceSampleRatio: 1, TraceBatchSize: DefaultTraceBatchSize, TraceQueue: 512, TraceConcurrency: 2, TraceTimeout: 3 * time.Second}
}

func (c Config) Validate() error {
	if c.MaxSeries < 1 || c.MaxSeries > 4096 || c.MaxRecent < 0 || c.MaxRecent > 4096 || c.MaxActive < 1 || c.MaxActive > 1<<20 || c.ErrorQueue < 1 || c.ErrorQueue > 4096 || c.ReporterConcurrency < 1 || c.ReporterConcurrency > 16 || c.ReporterTimeout <= 0 || c.ReporterTimeout > time.Minute {
		return fault.New(fault.Invalid, "invalid observability resource bounds")
	}
	if c.TraceQueue < 1 || c.TraceQueue > 4096 || c.TraceConcurrency < 1 || c.TraceConcurrency > 16 || c.TraceTimeout <= 0 || c.TraceTimeout > time.Minute || len(c.TraceExporters)+len(c.TraceBatchExporters) > 16 || c.TraceBatchSize < 0 || c.TraceBatchSize > 512 {
		return fault.New(fault.Invalid, "invalid trace export resource bounds")
	}
	if !(c.TraceSampleRatio >= 0 && c.TraceSampleRatio <= 1) {
		return fault.New(fault.Invalid, "trace sampling ratio must be between 0 and 1")
	}
	for _, exporter := range c.TraceExporters {
		if exporter == nil {
			return fault.New(fault.Invalid, "trace exporter cannot be nil")
		}
	}
	for _, exporter := range c.TraceBatchExporters {
		if exporter == nil {
			return fault.New(fault.Invalid, "trace batch exporter cannot be nil")
		}
	}
	return nil
}

// sampler resolves the local sampling policy. Zero ratio selects every trace.
func (c Config) sampler() tracing.Sampler {
	ratio := c.TraceSampleRatio
	if !c.SampleTraces {
		ratio = 0
	} else if ratio == 0 {
		ratio = 1
	}
	sampler, _ := tracing.RatioSampler(ratio)
	return sampler
}

func (c Config) batchSize() int {
	if c.TraceBatchSize == 0 {
		return DefaultTraceBatchSize
	}
	return c.TraceBatchSize
}
