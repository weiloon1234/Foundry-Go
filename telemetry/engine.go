package telemetry

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// Dependencies are borrowed for the Engine's entire lifetime, through Done.
// Schema is explicit. Recorder must have the collector's reporter registered by
// its owner. Nil Readiness means unavailable; nil Logger disables failure logs.
type Dependencies struct {
	DB        *database.DB
	Schema    string
	Recorder  *observability.Recorder
	Readiness *diagnostics.Runtime
	Clock     clock.Clock
	Logger    *slog.Logger
}

// Engine owns one collector, capture loop and final drain. It never closes its
// dependencies. Run is single-use; its return does not finalize the collector.
// Call Close after producers stop, and retain dependencies until Done closes.
// Do not copy an Engine.
type Engine struct {
	collector        *Collector
	deps             Dependencies
	config           Config
	storage          *storage
	reader           *Reader
	operations       operationOwner
	mu               sync.Mutex
	started, closing bool
	cancel           context.CancelFunc
	runDone, done    chan struct{}
	closeErr         error
}

// New validates and snapshots policy before claiming the collector. It performs
// no database I/O and starts no work. A collector has exactly one Engine/storage
// owner, including when that owner is closed. Apply Migrations separately.
func New(c *Collector, deps Dependencies, config Config) (*Engine, error) {
	if c == nil || deps.DB == nil || deps.Recorder == nil || credential.IsNil(deps.Clock) {
		return nil, invalid("telemetry requires a collector, database, recorder and clock")
	}
	if err := c.config.Validate(); err != nil {
		return nil, err
	}
	config.Alerts.Rules = slices.Clone(config.Alerts.Rules)
	e := &Engine{collector: c, deps: deps, config: config, runDone: make(chan struct{}), done: make(chan struct{})}
	reader, err := newReader(deps, config, c.config.Routes, &e.operations)
	if err != nil {
		return nil, err
	}
	s, err := newStorage(c, deps.DB, deps.Schema, config)
	if err != nil {
		return nil, err
	}
	e.reader, e.storage = reader, s
	return e, nil
}

// Run captures and persists periodically until cancellation or Close. Ordinary
// capture, storage and prune failures are counted and retried on later ticks;
// they do not terminate a foundation critical task. Final capture belongs to Close.
func (e *Engine) Run(ctx context.Context) error { return e.run(ctx, nil) }

func (e *Engine) run(ctx context.Context, ticks <-chan time.Time) error {
	if e == nil || e.done == nil || ctx == nil {
		return invalid("telemetry run requires an initialized engine and context")
	}
	e.mu.Lock()
	if e.started || e.closing {
		e.mu.Unlock()
		return safeerror.NewFault(fault.Conflict, "telemetry run is single-use and cannot start after close")
	}
	e.started = true
	lifetime, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.mu.Unlock()
	defer cancel()
	defer close(e.runDone)
	return callback.Isolated("telemetry loop", func() error { return e.loop(lifetime, ticks) })
}

func (e *Engine) loop(ctx context.Context, ticks <-chan time.Time) error {
	if ticks == nil {
		ticker := time.NewTicker(e.config.FlushInterval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	var lastPrune time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticks:
			if ctx.Err() != nil {
				return nil
			}
			if err := e.capture(ctx); err != nil {
				e.captureFailure(err)
			}
			if err := e.drain(ctx); err != nil {
				e.logFailure(err)
			}
			if ctx.Err() == nil && (lastPrune.IsZero() || time.Since(lastPrune) >= e.config.Retention.PruneInterval) {
				if _, err := e.storage.prune(ctx); err != nil {
					e.logFailure(err)
				}
				lastPrune = time.Now()
			}
		}
	}
}

// Close seals intake and API admission once, cancels Run, and starts finalization.
// Its context bounds only this caller's wait. Finalization waits for actual work
// exit, performs a final capture and a DrainTimeout-limited FIFO drain, and counts
// remaining batches as best-effort shutdown loss. Late reports also count loss.
// Repeated Close calls return the same completed result; Done is the ownership barrier.
func (e *Engine) Close(ctx context.Context) error {
	if e == nil || e.done == nil || ctx == nil {
		return invalid("telemetry close requires an initialized engine and context")
	}
	e.mu.Lock()
	if !e.closing {
		e.closing = true
		e.collector.mu.Lock()
		e.collector.sealed = true
		e.collector.mu.Unlock()
		operations := e.operations.seal()
		if e.cancel != nil {
			e.cancel()
		}
		started := e.started
		go e.finalize(started, operations)
	}
	e.mu.Unlock()
	select {
	case <-e.done:
		return e.closedResult()
	default:
	}
	select {
	case <-e.done:
		return e.closedResult()
	case <-ctx.Done():
		return safeerror.WrapFault(fault.Timeout, "telemetry shutdown is still pending", ctx.Err())
	}
}

func (e *Engine) finalize(started bool, operations <-chan struct{}) {
	if started {
		<-e.runDone
	}
	<-operations
	ctx, cancel := context.WithTimeout(context.Background(), e.config.DrainTimeout)
	err := e.capture(ctx)
	if err != nil {
		e.captureFailure(err)
	}
	drainErr := e.drain(ctx)
	if drainErr != nil {
		e.logFailure(drainErr)
	}
	cancel()
	c := e.collector
	c.mu.Lock()
	for _, pending := range c.pending {
		c.dropBatchLocked(pending)
	}
	clear(c.pending)
	c.pending = nil
	// Capture can fail before draining intake, for example on a clock callback.
	c.dropLocked(uint64(len(c.issues)))
	c.issues = nil
	clear(c.issueCounts)
	metrics := c.metrics.Drain()
	c.dropLocked(metrics.Dropped)
	for _, metric := range metrics.Series {
		c.dropLocked(metric.Count)
	}
	c.mu.Unlock()
	e.mu.Lock()
	e.closeErr = safeerror.Join("telemetry finalization", err, drainErr)
	close(e.done)
	e.mu.Unlock()
}

func (e *Engine) closedResult() error { e.mu.Lock(); defer e.mu.Unlock(); return e.closeErr }

// Done closes only after the loop, admitted API work and finalization have
// actually exited. A Close deadline never releases ownership early.
func (e *Engine) Done() <-chan struct{} { return e.done }

// Reader returns the engine's bounded read service. It shares Engine lifetime
// admission; requests after Close begins fail without touching the database.
func (e *Engine) Reader() *Reader { return e.reader }

// RunID returns this process's telemetry run identity, the same ID that its
// persisted run, buckets and receipts carry. Applications use it to bind
// process-local operations, such as log maintenance, to the current owner. An
// uninitialized engine returns the zero ID, which matches no run.
func (e *Engine) RunID() model.ID[Run] {
	if e == nil || e.collector == nil {
		return model.ID[Run]{}
	}
	return e.collector.RunID()
}

// RecordBrowser admits an untrusted browser report through the engine-owned
// collector. It preserves the collector's validation and bounded loss accounting;
// success is not a persistence receipt. Reports after Close begins count as
// shutdown loss and return nil without reopening intake or touching the database.
// Applications retain CSRF, body limits, rate limiting and portal authorization.
func (e *Engine) RecordBrowser(ctx context.Context, report BrowserError) error {
	if e == nil || e.collector == nil || e.done == nil {
		return invalid("telemetry browser admission requires an initialized engine")
	}
	return e.collector.RecordBrowser(ctx, report)
}

// Prune deletes expired telemetry under the shared storage lock. It is suitable
// for an app-owned maintenance task and never deletes application audit data.
func (e *Engine) Prune(ctx context.Context) (PruneResult, error) {
	if e == nil || e.storage == nil || ctx == nil {
		return PruneResult{}, invalid("telemetry prune requires an engine and context")
	}
	if err := e.operations.enter(); err != nil {
		return PruneResult{}, err
	}
	defer e.operations.leave()
	return e.storage.prune(ctx)
}

func (e *Engine) drain(ctx context.Context) error {
	for {
		e.collector.mu.Lock()
		pending := len(e.collector.pending)
		e.collector.mu.Unlock()
		if pending == 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.storage.flush(ctx, nil); err != nil {
			return err
		}
	}
}

func (e *Engine) captureFailure(err error) {
	e.collector.mu.Lock()
	e.collector.failures = saturatingAdd(e.collector.failures, 1)
	e.collector.mu.Unlock()
	e.logFailure(err)
}

func (e *Engine) logFailure(err error) {
	if e.deps.Logger == nil {
		return
	}
	_ = callback.Isolated("telemetry logger", func() error {
		e.deps.Logger.LogAttrs(context.Background(), slog.LevelWarn, "telemetry operation failed", slog.Any("diagnostic", errordiag.Describe(err)))
		return nil
	})
}
