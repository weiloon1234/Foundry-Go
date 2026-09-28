package observability

import (
	"context"
	"math"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

var durationBounds = [...]time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2500 * time.Millisecond, 5 * time.Second, 10 * time.Second, time.Minute}

func DurationBuckets() []time.Duration { return slices.Clone(durationBounds[:]) }

type metricKey struct {
	Operation Operation
	Result    Result
}

type Metric struct {
	Operation Operation                   `json:"operation"`
	Result    Result                      `json:"result"`
	Count     uint64                      `json:"count"`
	Seconds   float64                     `json:"seconds"`
	Buckets   [len(durationBounds)]uint64 `json:"buckets"`
}

type Snapshot struct {
	Closing             bool             `json:"closing"`
	Mode                maintenance.Mode `json:"mode"`
	Active              int              `json:"active"`
	Completed           uint64           `json:"completed"`
	Failures            uint64           `json:"failures"`
	DroppedSeries       uint64           `json:"dropped_series"`
	DroppedSpans        uint64           `json:"dropped_spans"`
	DroppedReports      uint64           `json:"dropped_reports"`
	ReporterFailures    uint64           `json:"reporter_failures"`
	PendingReports      int              `json:"pending_reports"`
	DroppedTraces       uint64           `json:"dropped_traces"`
	TraceExportFailures uint64           `json:"trace_export_failures"`
	PendingTraces       int              `json:"pending_traces"`
	Metrics             []Metric         `json:"metrics"`
	Recent              []Entry          `json:"recent"`
}

// Recorder is application-owned. New performs no I/O and starts no goroutines.
// Run owns reporter workers; Close rejects new spans and drains accepted spans
// and reporter callbacks. No global logger/provider is modified. Do not copy it.
type Recorder struct {
	config                                                                             Config
	reporters                                                                          []Reporter
	gate                                                                               *maintenance.Gate
	mu                                                                                 sync.Mutex
	closing, running, queueClosed, claimed                                             bool
	active                                                                             int
	completed, failures, droppedSeries, droppedSpans, droppedReports, reporterFailures uint64
	droppedTraces, traceExportFailures                                                 uint64
	metrics                                                                            map[metricKey]Metric
	recent                                                                             []Entry
	recentNext                                                                         int
	queue                                                                              chan ErrorReport
	traces                                                                             chan Entry
	done                                                                               chan struct{}
	ready                                                                              chan struct{}
}

func New(config Config, reporters ...Reporter) (*Recorder, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if len(reporters) > 16 {
		return nil, fault.New(fault.Invalid, "error reporter count exceeds its bound")
	}
	for _, reporter := range reporters {
		if reporter == nil {
			return nil, fault.New(fault.Invalid, "error reporter cannot be nil")
		}
	}
	gate := config.Maintenance
	if gate == nil {
		gate = &maintenance.Gate{}
	}
	config.TraceExporters = slices.Clone(config.TraceExporters)
	return &Recorder{config: config, reporters: slices.Clone(reporters), gate: gate, metrics: make(map[metricKey]Metric), queue: make(chan ErrorReport, config.ErrorQueue), traces: make(chan Entry, config.TraceQueue), done: make(chan struct{}), ready: make(chan struct{})}, nil
}

func increment(value *uint64) {
	if *value < math.MaxUint64 {
		*value = *value + 1
	}
}

// Span retains one recorder admission until End. End is concurrency-safe and
// idempotent; forgetting it retains shutdown ownership. Nil spans are harmless.
// Do not copy a Span.
type Span struct {
	once     sync.Once
	recorder *Recorder
	entry    Entry
	started  time.Time
}

// Start captures one operation and derives a trace child. A nil optional
// recorder preserves the input context and returns a nil span. Resource limits
// return an error and increment dropped-span accounting; instrumentation callers
// may continue their business operation without a span.
func (r *Recorder) Start(ctx context.Context, operation Operation) (context.Context, *Span, error) {
	if r == nil {
		return ctx, nil, nil
	}
	if r.done == nil || ctx == nil {
		return ctx, nil, fault.New(fault.Invalid, "observation requires a recorder and context")
	}
	if err := operation.Validate(); err != nil {
		r.mu.Lock()
		increment(&r.droppedSpans)
		r.mu.Unlock()
		return ctx, nil, err
	}
	r.mu.Lock()
	if r.closing {
		increment(&r.droppedSpans)
		r.mu.Unlock()
		return ctx, nil, fault.New(fault.Closed, "observation admission is closed")
	}
	if r.active >= r.config.MaxActive {
		increment(&r.droppedSpans)
		r.mu.Unlock()
		return ctx, nil, fault.New(fault.Conflict, "observation admission exceeds its bound")
	}
	r.active++
	r.mu.Unlock()
	parent := tracing.FromContext(ctx)
	work, trace, err := tracing.Start(ctx, r.config.SampleTraces)
	if err != nil {
		r.mu.Lock()
		r.active--
		increment(&r.droppedSpans)
		r.finishAdmissionLocked()
		r.mu.Unlock()
		return ctx, nil, err
	}
	started := time.Now()
	entry := Entry{Operation: operation, Started: started.UTC(), TraceID: trace.TraceID(), SpanID: trace.SpanID(), ParentID: parent.SpanID(), RequestID: attribution.FromContext(ctx).Request().ID}
	return WithContext(work, r), &Span{recorder: r, entry: entry, started: started}, nil
}

func (s *Span) End(result Result) {
	if s == nil || s.recorder == nil {
		return
	}
	s.once.Do(func() {
		r := s.recorder
		entry := s.entry
		entry.Duration = max(time.Duration(0), time.Since(s.started))
		normalized, err := result.normalize(entry.Operation.Kind)
		r.mu.Lock()
		defer r.mu.Unlock()
		r.active--
		if err != nil {
			increment(&r.droppedSpans)
			r.finishAdmissionLocked()
			return
		}
		entry.Result = normalized
		r.recordLocked(entry)
		r.finishAdmissionLocked()
	})
}

func (r *Recorder) recordLocked(entry Entry) {
	increment(&r.completed)
	failed := entry.Result.Outcome == Failed || entry.Result.Outcome == Panicked || entry.Result.Outcome == TimedOut
	if failed {
		increment(&r.failures)
	}
	key := metricKey{Operation: entry.Operation, Result: entry.Result}
	metric, exists := r.metrics[key]
	if exists || len(r.metrics) < r.config.MaxSeries {
		metric.Operation, metric.Result = entry.Operation, entry.Result
		increment(&metric.Count)
		metric.Seconds = min(math.MaxFloat64, metric.Seconds+entry.Duration.Seconds())
		for i, bound := range durationBounds {
			if entry.Duration <= bound {
				increment(&metric.Buckets[i])
			}
		}
		r.metrics[key] = metric
	} else {
		increment(&r.droppedSeries)
	}
	if r.config.MaxRecent > 0 {
		if len(r.recent) < r.config.MaxRecent {
			r.recent = append(r.recent, entry)
		} else {
			r.recent[r.recentNext] = entry
			r.recentNext = (r.recentNext + 1) % len(r.recent)
		}
	}
	if failed && len(r.reporters) > 0 {
		select {
		case r.queue <- ErrorReport{Entry: entry}:
		default:
			increment(&r.droppedReports)
		}
	}
	if r.config.SampleTraces && len(r.config.TraceExporters) > 0 {
		select {
		case r.traces <- entry:
		default:
			increment(&r.droppedTraces)
		}
	}
}

func (r *Recorder) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{Mode: maintenance.Serving, Metrics: []Metric{}, Recent: []Entry{}}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := Snapshot{Closing: r.closing, Mode: r.gate.Mode(), Active: r.active, Completed: r.completed, Failures: r.failures, DroppedSeries: r.droppedSeries, DroppedSpans: r.droppedSpans, DroppedReports: r.droppedReports, ReporterFailures: r.reporterFailures, PendingReports: len(r.queue), Metrics: make([]Metric, 0, len(r.metrics)), Recent: make([]Entry, 0, len(r.recent))}
	result.DroppedTraces, result.TraceExportFailures, result.PendingTraces = r.droppedTraces, r.traceExportFailures, len(r.traces)
	for _, metric := range r.metrics {
		result.Metrics = append(result.Metrics, metric)
	}
	sort.Slice(result.Metrics, func(i, j int) bool {
		a, b := result.Metrics[i], result.Metrics[j]
		if a.Operation.Kind != b.Operation.Kind {
			return a.Operation.Kind < b.Operation.Kind
		}
		if a.Operation.Name != b.Operation.Name {
			return a.Operation.Name < b.Operation.Name
		}
		if a.Result.Outcome != b.Result.Outcome {
			return a.Result.Outcome < b.Result.Outcome
		}
		return a.Result.Status < b.Result.Status
	})
	result.Recent = append(result.Recent, r.recent[r.recentNext:]...)
	result.Recent = append(result.Recent, r.recent[:r.recentNext]...)
	return result
}

func (r *Recorder) Gate() *maintenance.Gate {
	if r == nil {
		return nil
	}
	return r.gate
}

// Claim reserves a fresh recorder for one application builder without starting
// resources. Failed/abandoned builders still consume this reservation; create
// a fresh recorder for another builder. This prevents accidental cross-app
// shutdown of the same recorder. Direct standalone Run needs no reservation.
func (r *Recorder) Claim() error {
	if r == nil || r.done == nil {
		return fault.New(fault.Invalid, "observation ownership requires an initialized recorder")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimed || r.running || r.closing {
		return fault.New(fault.Conflict, "observation recorder is already owned, started or closed")
	}
	r.claimed = true
	return nil
}

type recorderKey struct{}

// WithContext carries only the explicit application recorder. Kernels that
// intentionally discard parent context values may capture this specific runtime
// capability before creating their fresh execution contexts. A nil recorder
// clears an inherited recorder without altering other context values.
func WithContext(ctx context.Context, recorder *Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, recorder)
}
func FromContext(ctx context.Context) *Recorder {
	if ctx == nil {
		return nil
	}
	recorder, _ := ctx.Value(recorderKey{}).(*Recorder)
	return recorder
}
