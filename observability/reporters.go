package observability

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Run owns reporter workers once. Parent cancellation seals admission and drains
// accepted spans/reports; deadlines remain cooperative and cannot abandon live
// callbacks. Run returns only after reporter work actually exits.
func (r *Recorder) Run(ctx context.Context) error {
	if r == nil || r.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "reporters require a recorder and context")
	}
	r.life.Lock()
	if r.running || r.closing() {
		r.life.Unlock()
		return fault.New(fault.Closed, "reporter runtime already started or closed")
	}
	if err := ctx.Err(); err != nil {
		r.life.Unlock()
		return err
	}
	r.running = true
	close(r.ready)
	r.life.Unlock()
	stop := context.AfterFunc(ctx, r.seal)
	defer stop()
	var workers sync.WaitGroup
	startReportWorkers(&workers, r.queue, r.config.ReporterConcurrency, r.config.ReporterTimeout, r.reporters, &r.reporterFailures)
	startTraceWorkers(&workers, r.traces, r.config, &r.traceFailures)
	workers.Wait()
	r.life.Lock()
	close(r.done)
	r.life.Unlock()
	return nil
}

// exportOne runs one callback with its own isolation and deadline. A callback
// that fails or ignores its deadline counts one failure.
func exportOne(timeout time.Duration, failures *atomic.Uint64, export func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := callback.Isolated("export observation", func() error { return export(ctx) })
	failed := err != nil || ctx.Err() != nil
	cancel()
	if failed {
		increment(failures)
	}
}

func startReportWorkers(workers *sync.WaitGroup, queue <-chan ErrorReport, concurrency int, timeout time.Duration, reporters []Reporter, failures *atomic.Uint64) {
	// A no-reporter queue still has one consumer so shutdown always waits for
	// admission to close it. Such queues never receive data from record.
	if len(reporters) == 0 {
		concurrency = 1
	}
	for range concurrency {
		workers.Go(func() {
			for report := range queue {
				for _, reporter := range reporters {
					exportOne(timeout, failures, func(ctx context.Context) error { return reporter(ctx, report) })
				}
			}
		})
	}
}

// Trace workers never wait to fill a batch: they take one queued entry, then
// only entries that are already queued, up to the configured batch size.
func startTraceWorkers(workers *sync.WaitGroup, queue <-chan Entry, config Config, failures *atomic.Uint64) {
	concurrency := config.TraceConcurrency
	if len(config.TraceExporters)+len(config.TraceBatchExporters) == 0 {
		concurrency = 1
	}
	size := config.batchSize()
	for range concurrency {
		workers.Go(func() {
			batch := make([]Entry, 0, size)
			for first := range queue {
				batch = append(batch[:0], first)
			fill:
				for len(batch) < size {
					select {
					case entry, open := <-queue:
						if !open {
							break fill
						}
						batch = append(batch, entry)
					default:
						break fill
					}
				}
				for _, entry := range batch {
					for _, exporter := range config.TraceExporters {
						exportOne(config.TraceTimeout, failures, func(ctx context.Context) error { return exporter(ctx, entry) })
					}
				}
				for _, exporter := range config.TraceBatchExporters {
					owned := slices.Clone(batch)
					exportOne(config.TraceTimeout, failures, func(ctx context.Context) error { return exporter(ctx, owned) })
				}
			}
		})
	}
}

// finishAdmission runs once, after sealing and after the last admitted span
// released. No span can enqueue afterwards, so closing the queues is safe.
func (r *Recorder) finishAdmission() {
	r.finished.Do(func() {
		close(r.queue)
		close(r.traces)
		r.life.Lock()
		defer r.life.Unlock()
		if r.running {
			return
		}
		// No reporter worker was ever started. Preserve honest drop accounting
		// instead of running user callbacks unexpectedly from Close.
		for range r.queue {
			increment(&r.droppedReports)
		}
		for range r.traces {
			increment(&r.droppedTraces)
		}
		close(r.ready)
		close(r.done)
	})
}

func (r *Recorder) seal() {
	r.gate.Drain()
	for {
		current := r.state.Load()
		if current&closingBit != 0 {
			return
		}
		if r.state.CompareAndSwap(current, current|closingBit) {
			if current == 0 {
				r.finishAdmission()
			}
			return
		}
	}
}

// Close rejects new spans immediately. A deadline bounds the caller's wait;
// Done remains open while any span or accepted reporter callback still runs.
func (r *Recorder) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "observation close requires a context and initialized recorder")
	}
	r.seal()
	select {
	case <-r.done:
		return nil
	default:
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Recorder) Done() <-chan struct{} { return r.done }

// Ready waits until reporter admission has started or the recorder has closed.
// It reports runtime startup, not application or dependency readiness.
func (r *Recorder) Ready(ctx context.Context) error {
	if r == nil || r.ready == nil || ctx == nil {
		return fault.New(fault.Invalid, "reporter readiness requires a recorder and context")
	}
	select {
	case <-r.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	r.life.Lock()
	defer r.life.Unlock()
	if !r.running || r.closing() {
		return fault.New(fault.Closed, "reporter runtime is closed")
	}
	return nil
}
