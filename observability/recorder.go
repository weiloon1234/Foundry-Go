package observability

import (
	"cmp"
	"context"
	"hash/maphash"
	"math"
	"slices"
	"sync"
	"sync/atomic"
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
	CollectorFailures   uint64           `json:"collector_failures"`
	Metrics             []Metric         `json:"metrics"`
	Recent              []Entry          `json:"recent"`
}

// closingBit marks sealed admission in Recorder.state; the low bits count
// admitted, unfinished spans. MaxActive stays far below this bit.
const closingBit = uint64(1) << 62

// metricShards spreads series lookup across independent locks. A series is
// created once under its shard lock; later observations only use atomics.
const metricShards = 16

type metricShard struct {
	mu     sync.RWMutex
	series map[metricKey]*series
}

// series stores non-cumulative bucket counts. Updates increment Count before
// the bucket and snapshots read buckets before Count, so an exported
// cumulative bucket never exceeds its histogram count.
type series struct {
	count   atomic.Uint64
	nanos   atomic.Uint64
	buckets [len(durationBounds)]atomic.Uint64
}

type recentSlot struct {
	mu    sync.Mutex
	seq   uint64
	entry Entry
}

// Recorder is application-owned. New performs no I/O and starts no goroutines.
// Run owns reporter workers; Close rejects new spans and drains accepted spans
// and reporter callbacks. No global logger/provider is modified. Do not copy it.
// Span admission and completion use atomics and per-series shards; they never
// wait for snapshot formatting, collectors or exporter callbacks.
type Recorder struct {
	config    Config
	reporters []Reporter
	gate      *maintenance.Gate
	sampler   tracing.Sampler
	exporting bool
	seed      maphash.Seed

	// state packs the closing flag with the admitted span count.
	state    atomic.Uint64
	finished sync.Once

	// life protects the reporter runtime lifecycle and done/ready closure.
	life              sync.Mutex
	running, claimed  bool
	completed         atomic.Uint64
	failures          atomic.Uint64
	droppedSeries     atomic.Uint64
	droppedSpans      atomic.Uint64
	droppedReports    atomic.Uint64
	reporterFailures  atomic.Uint64
	droppedTraces     atomic.Uint64
	traceFailures     atomic.Uint64
	collectorFailures atomic.Uint64
	seriesCount       atomic.Int64
	shards            [metricShards]metricShard

	// recent is a ring of individually locked slots; writers claim a slot by
	// sequence number so concurrent spans never wait on one shared lock.
	recent     []recentSlot
	recentNext atomic.Uint64

	collectorsMu sync.Mutex
	collectors   []namedCollector

	queue  chan ErrorReport
	traces chan Entry
	done   chan struct{}
	ready  chan struct{}
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
	config.TraceBatchExporters = slices.Clone(config.TraceBatchExporters)
	r := &Recorder{config: config, reporters: slices.Clone(reporters), gate: gate, sampler: config.sampler(), exporting: len(config.TraceExporters)+len(config.TraceBatchExporters) > 0, seed: maphash.MakeSeed(), queue: make(chan ErrorReport, config.ErrorQueue), traces: make(chan Entry, config.TraceQueue), done: make(chan struct{}), ready: make(chan struct{})}
	for i := range r.shards {
		r.shards[i].series = make(map[metricKey]*series)
	}
	r.recent = make([]recentSlot, config.MaxRecent)
	return r, nil
}

// increment saturates instead of wrapping so overflow can never report a
// smaller total than was actually observed.
func increment(value *atomic.Uint64) { add(value, 1) }
func add(value *atomic.Uint64, delta uint64) {
	for {
		current := value.Load()
		next := current + delta
		if next < current {
			next = math.MaxUint64
		}
		if current == next || value.CompareAndSwap(current, next) {
			return
		}
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
	sampled  bool
}

func (r *Recorder) admit() error {
	for {
		current := r.state.Load()
		if current&closingBit != 0 {
			return fault.New(fault.Closed, "observation admission is closed")
		}
		if current >= uint64(r.config.MaxActive) {
			return fault.New(fault.Conflict, "observation admission exceeds its bound")
		}
		if r.state.CompareAndSwap(current, current+1) {
			return nil
		}
	}
}

// release ends one admission. The last span after sealing closes the queues.
func (r *Recorder) release() {
	if r.state.Add(^uint64(0)) == closingBit {
		r.finishAdmission()
	}
}

// Start captures one operation and derives a trace child. A nil optional
// recorder preserves the input context and returns a nil span. Resource limits
// return an error and increment dropped-span accounting; instrumentation callers
// may continue their business operation without a span. An already-cancelled
// context is still observed: cancellation is the operation's outcome to report.
func (r *Recorder) Start(ctx context.Context, operation Operation) (context.Context, *Span, error) {
	if r == nil {
		return ctx, nil, nil
	}
	if r.done == nil || ctx == nil {
		return ctx, nil, fault.New(fault.Invalid, "observation requires a recorder and context")
	}
	if err := operation.Validate(); err != nil {
		increment(&r.droppedSpans)
		return ctx, nil, err
	}
	if err := r.admit(); err != nil {
		increment(&r.droppedSpans)
		return ctx, nil, err
	}
	parent := tracing.FromContext(ctx)
	work, trace, err := tracing.StartSampled(ctx, r.sampler)
	if err != nil {
		increment(&r.droppedSpans)
		r.release()
		return ctx, nil, err
	}
	started := time.Now()
	entry := Entry{Operation: operation, Started: started.UTC(), TraceID: trace.TraceID(), SpanID: trace.SpanID(), ParentID: parent.SpanID(), RequestID: attribution.FromContext(ctx).Request().ID}
	return WithContext(work, r), &Span{recorder: r, entry: entry, started: started, sampled: trace.Sampled()}, nil
}

func (s *Span) End(result Result) { s.EndWithDiagnostic(result, fault.Diagnostic{}) }

// EndWithDiagnostic completes the span and attaches a redacted failure summary
// to the error report produced for a failed, panicked or timed-out result.
func (s *Span) EndWithDiagnostic(result Result, diagnostic fault.Diagnostic) {
	if s == nil || s.recorder == nil {
		return
	}
	s.once.Do(func() {
		r := s.recorder
		// Queue admission happens before release: the queues close only once
		// the last admitted span has released after sealing.
		defer r.release()
		entry := s.entry
		entry.Duration = max(time.Duration(0), time.Since(s.started))
		normalized, err := result.normalize(entry.Operation.Kind)
		if err != nil {
			increment(&r.droppedSpans)
			return
		}
		entry.Result = normalized
		r.record(entry, diagnostic, s.sampled)
	})
}

func (r *Recorder) record(entry Entry, diagnostic fault.Diagnostic, sampled bool) {
	increment(&r.completed)
	failed := entry.Result.Outcome == Failed || entry.Result.Outcome == Panicked || entry.Result.Outcome == TimedOut
	if failed {
		increment(&r.failures)
	}
	r.observe(metricKey{Operation: entry.Operation, Result: entry.Result}, entry.Duration)
	if len(r.recent) > 0 {
		seq := r.recentNext.Add(1)
		slot := &r.recent[(seq-1)%uint64(len(r.recent))]
		slot.mu.Lock()
		// A delayed writer never replaces a newer entry in its slot.
		if seq > slot.seq {
			slot.seq, slot.entry = seq, entry
		}
		slot.mu.Unlock()
	}
	if failed && len(r.reporters) > 0 {
		select {
		case r.queue <- ErrorReport{Entry: entry, Diagnostic: diagnostic.Clone()}:
		default:
			increment(&r.droppedReports)
		}
	}
	if sampled && r.exporting {
		select {
		case r.traces <- entry:
		default:
			increment(&r.droppedTraces)
		}
	}
}

func (r *Recorder) observe(key metricKey, duration time.Duration) {
	shard := &r.shards[maphash.Comparable(r.seed, key)%metricShards]
	shard.mu.RLock()
	item := shard.series[key]
	shard.mu.RUnlock()
	if item == nil {
		shard.mu.Lock()
		if item = shard.series[key]; item == nil {
			if !r.reserveSeries() {
				shard.mu.Unlock()
				increment(&r.droppedSeries)
				return
			}
			item = &series{}
			shard.series[key] = item
		}
		shard.mu.Unlock()
	}
	increment(&item.count)
	add(&item.nanos, uint64(duration))
	for i, bound := range durationBounds {
		if duration <= bound {
			increment(&item.buckets[i])
			break
		}
	}
}

func (r *Recorder) reserveSeries() bool {
	for {
		current := r.seriesCount.Load()
		if current >= int64(r.config.MaxSeries) {
			return false
		}
		if r.seriesCount.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (r *Recorder) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{Mode: maintenance.Serving, Metrics: []Metric{}, Recent: []Entry{}}
	}
	state := r.state.Load()
	result := Snapshot{Closing: state&closingBit != 0, Mode: r.gate.Mode(), Active: int(state &^ closingBit), Completed: r.completed.Load(), Failures: r.failures.Load(), DroppedSeries: r.droppedSeries.Load(), DroppedSpans: r.droppedSpans.Load(), DroppedReports: r.droppedReports.Load(), ReporterFailures: r.reporterFailures.Load(), PendingReports: len(r.queue), DroppedTraces: r.droppedTraces.Load(), TraceExportFailures: r.traceFailures.Load(), PendingTraces: len(r.traces), CollectorFailures: r.collectorFailures.Load()}
	result.Metrics = make([]Metric, 0, r.seriesCount.Load())
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.RLock()
		for key, item := range shard.series {
			result.Metrics = append(result.Metrics, item.snapshot(key))
		}
		shard.mu.RUnlock()
	}
	type ordered struct {
		seq   uint64
		entry Entry
	}
	recent := make([]ordered, 0, len(r.recent))
	for i := range r.recent {
		slot := &r.recent[i]
		slot.mu.Lock()
		if slot.seq != 0 {
			recent = append(recent, ordered{slot.seq, slot.entry})
		}
		slot.mu.Unlock()
	}
	// Sorting runs outside every recorder lock; recent entries are oldest first.
	slices.SortFunc(recent, func(a, b ordered) int { return cmp.Compare(a.seq, b.seq) })
	result.Recent = make([]Entry, len(recent))
	for i, item := range recent {
		result.Recent[i] = item.entry
	}
	slices.SortFunc(result.Metrics, func(a, b Metric) int {
		return cmp.Or(cmp.Compare(a.Operation.Kind, b.Operation.Kind), cmp.Compare(a.Operation.Name, b.Operation.Name), cmp.Compare(a.Result.Outcome, b.Result.Outcome), cmp.Compare(a.Result.Status, b.Result.Status))
	})
	return result
}

func (s *series) snapshot(key metricKey) Metric {
	metric := Metric{Operation: key.Operation, Result: key.Result}
	var cumulative uint64
	for i := range s.buckets {
		cumulative += s.buckets[i].Load()
		metric.Buckets[i] = cumulative
	}
	metric.Count = s.count.Load()
	metric.Seconds = float64(s.nanos.Load()) / float64(time.Second)
	return metric
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
	r.life.Lock()
	defer r.life.Unlock()
	if r.claimed || r.running || r.closing() {
		return fault.New(fault.Conflict, "observation recorder is already owned, started or closed")
	}
	r.claimed = true
	return nil
}

func (r *Recorder) closing() bool { return r.state.Load()&closingBit != 0 }

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
