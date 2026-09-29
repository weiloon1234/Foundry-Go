package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/value"
)

// InlineBackend is optional. An inline authority (the sync driver, jobs/inline)
// makes its dispatcher run accepted jobs synchronously in the caller instead of
// waiting for a worker kernel.
type InlineBackend interface {
	JobInline() bool
}

// MaxInlineJobs bounds the jobs one inline run executes, including jobs that
// handlers dispatch while it runs.
const MaxInlineJobs = 1000

type inlineRunKey struct{}

// inlineRun collects the queues touched while one caller runs inline jobs, so
// a handler's own dispatches run after it returns instead of recursing.
type inlineRun struct {
	dispatcher *Dispatcher
	mu         sync.Mutex
	keys       []Key
}

func (r *inlineRun) add(key Key) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.keys {
		if existing == key {
			return
		}
	}
	r.keys = append(r.keys, key)
}
func (r *inlineRun) first() (Key, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.keys) == 0 {
		return Key{}, false
	}
	return r.keys[0], true
}
func (r *inlineRun) drop(key Key) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.keys {
		if existing == key {
			r.keys = append(r.keys[:i], r.keys[i+1:]...)
			return
		}
	}
}

func (d *Dispatcher) inline() bool {
	backend, ok := d.backend.(InlineBackend)
	return ok && backend.JobInline()
}

// runInline executes accepted work after the dispatch admission slot is freed.
// A dispatch from a handler of the same inline run only records its queue.
// Handler outcomes are recorded on the jobs, never returned to the dispatcher.
func (d *Dispatcher) runInline(ctx context.Context, key Key) {
	if !d.inline() {
		return
	}
	if run, ok := ctx.Value(inlineRunKey{}).(*inlineRun); ok && run.dispatcher == d {
		run.add(key)
		return
	}
	_, _ = d.drainInline(ctx, []Key{key})
}

// RunPending executes jobs of queue that are available now, synchronously in
// the caller, when the backend is an inline authority (the sync driver). Jobs
// released with a delay (admission, rate limits, retries) run in a later call
// once due. It returns the number of attempts processed.
func (d *Dispatcher) RunPending(ctx context.Context, queue Queue) (int, error) {
	if d == nil || ctx == nil {
		return 0, fault.New(fault.Invalid, "inline execution requires a dispatcher and context")
	}
	if !d.inline() {
		return 0, fault.New(fault.Invalid, "job backend does not execute inline")
	}
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return 0, err
	}
	return d.drainInline(ctx, []Key{key})
}

func (d *Dispatcher) inlineWorker() (*Worker, error) {
	d.inlineOnce.Do(func() {
		config := DefaultWorkerConfig(d.config.Namespace, "inline")
		config.Concurrency, config.FailureLog = 1, false
		d.inlineWorkerValue, d.inlineErr = NewWorker(d.backend, d.registry, config)
	})
	return d.inlineWorkerValue, d.inlineErr
}

// drainInline reserves and processes available jobs one at a time, the same
// way a worker would (admission, middleware, retries, overlap and history), but
// in the caller's goroutine. The caller's cancellation cancels running handlers.
func (d *Dispatcher) drainInline(ctx context.Context, keys []Key) (int, error) {
	worker, err := d.inlineWorker()
	if err != nil {
		return 0, err
	}
	run := &inlineRun{dispatcher: d, keys: keys}
	work, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	work = maintenance.WithContext(observability.WithContext(work, observability.FromContext(ctx)), admissionGate(ctx))
	work = context.WithValue(work, inlineRunKey{}, run)
	state := workerRun{work: work, reserve: work}
	processed := 0
	for processed < MaxInlineJobs {
		key, ok := run.first()
		if !ok || work.Err() != nil {
			return processed, work.Err()
		}
		reservation, reservedAt, found, err := worker.reserveOne(state, key)
		if err != nil {
			return processed, err
		}
		if !found {
			run.drop(key)
			continue
		}
		worker.active.Add(1)
		worker.process(state, key, reservation, reservedAt)
		worker.active.Add(-1)
		processed++
	}
	return processed, nil
}

// reserveOne attempts one reservation from key with the worker's bounds.
func (w *Worker) reserveOne(run workerRun, key Key) (Reservation, time.Time, bool, error) {
	owner, err := lease.NewOwner()
	if err != nil {
		return Reservation{}, time.Time{}, false, err
	}
	operation, cancel := context.WithTimeout(run.work, w.config.OperationTimeout)
	defer cancel()
	reservedAt := time.Now()
	var found value.Optional[Reservation]
	err = callback.Isolated("reserve job", func() error {
		var err error
		found, err = w.backend.JobReserve(operation, key, owner, w.config.LeaseDuration)
		return err
	})
	if err != nil {
		return Reservation{}, time.Time{}, false, err
	}
	reservation, ok := found.Get()
	return reservation, reservedAt, ok, nil
}
