package observability

import (
	"context"
	"sync"
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
	r.mu.Lock()
	if r.running || r.closing {
		r.mu.Unlock()
		return fault.New(fault.Closed, "reporter runtime already started or closed")
	}
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return err
	}
	r.running = true
	close(r.ready)
	r.mu.Unlock()
	stop := context.AfterFunc(ctx, r.seal)
	defer stop()
	var workers sync.WaitGroup
	startExportWorkers(&workers, r, r.queue, r.config.ReporterConcurrency, r.config.ReporterTimeout, r.reporters, &r.reporterFailures)
	startExportWorkers(&workers, r, r.traces, r.config.TraceConcurrency, r.config.TraceTimeout, r.config.TraceExporters, &r.traceExportFailures)
	workers.Wait()
	r.mu.Lock()
	close(r.done)
	r.mu.Unlock()
	return nil
}

func startExportWorkers[T any, F ~func(context.Context, T) error](workers *sync.WaitGroup, recorder *Recorder, queue <-chan T, concurrency int, timeout time.Duration, exporters []F, failures *uint64) {
	// A no-exporter queue still has one consumer so shutdown always waits for
	// admission to close it. Such queues never receive data from recordLocked.
	if len(exporters) == 0 {
		concurrency = 1
	}
	for range concurrency {
		workers.Go(func() {
			for item := range queue {
				for _, exporter := range exporters {
					ctx, cancel := context.WithTimeout(context.Background(), timeout)
					err := callback.Isolated("export observation", func() error { return exporter(ctx, item) })
					failed := err != nil || ctx.Err() != nil
					cancel()
					if failed {
						recorder.mu.Lock()
						increment(failures)
						recorder.mu.Unlock()
					}
				}
			}
		})
	}
}

func (r *Recorder) finishAdmissionLocked() {
	if !r.closing || r.active != 0 || r.queueClosed {
		return
	}
	r.queueClosed = true
	close(r.queue)
	close(r.traces)
	if !r.running {
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
	}
}

func (r *Recorder) seal() {
	r.gate.Drain()
	r.mu.Lock()
	if !r.closing {
		r.closing = true
		r.finishAdmissionLocked()
	}
	r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running || r.closing {
		return fault.New(fault.Closed, "reporter runtime is closed")
	}
	return nil
}
