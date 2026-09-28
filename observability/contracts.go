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
}

type ErrorReport struct {
	Entry Entry `json:"entry"`
}

// Reporter receives owned metadata in a bounded worker. It must honor context
// cancellation and return only after its work ends. Recorder shutdown retains
// ownership of callbacks that ignore the deadline. Panics/Goexit are isolated.
type Reporter func(context.Context, ErrorReport) error

// TraceExporter receives sampled completed spans without payloads or vendor
// state. Exporters use their own bounded queue so trace traffic cannot evict
// error reports. Each callback must own its I/O until it returns.
type TraceExporter func(context.Context, Entry) error

type Config struct {
	MaxSeries           int
	MaxRecent           int
	MaxActive           int
	ErrorQueue          int
	ReporterConcurrency int
	ReporterTimeout     time.Duration
	SampleTraces        bool
	TraceExporters      []TraceExporter
	TraceQueue          int
	TraceConcurrency    int
	TraceTimeout        time.Duration
	Maintenance         *maintenance.Gate
}

func DefaultConfig() Config {
	return Config{MaxSeries: 512, MaxRecent: 128, MaxActive: 65536, ErrorQueue: 128, ReporterConcurrency: 2, ReporterTimeout: 3 * time.Second, SampleTraces: true, TraceQueue: 512, TraceConcurrency: 2, TraceTimeout: 3 * time.Second}
}

func (c Config) Validate() error {
	if c.MaxSeries < 1 || c.MaxSeries > 4096 || c.MaxRecent < 0 || c.MaxRecent > 4096 || c.MaxActive < 1 || c.MaxActive > 1<<20 || c.ErrorQueue < 1 || c.ErrorQueue > 4096 || c.ReporterConcurrency < 1 || c.ReporterConcurrency > 16 || c.ReporterTimeout <= 0 || c.ReporterTimeout > time.Minute {
		return fault.New(fault.Invalid, "invalid observability resource bounds")
	}
	if c.TraceQueue < 1 || c.TraceQueue > 4096 || c.TraceConcurrency < 1 || c.TraceConcurrency > 16 || c.TraceTimeout <= 0 || c.TraceTimeout > time.Minute || len(c.TraceExporters) > 16 {
		return fault.New(fault.Invalid, "invalid trace export resource bounds")
	}
	for _, exporter := range c.TraceExporters {
		if exporter == nil {
			return fault.New(fault.Invalid, "trace exporter cannot be nil")
		}
	}
	return nil
}
