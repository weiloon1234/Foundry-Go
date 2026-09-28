package jobs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Worker owns reservation loops and their handler lifetimes. Run is single-use;
// Stop cancels work and bounds only the caller's wait. Done closes only when
// every owned handler has actually returned, including context-ignoring ones.
// Close borrowed infrastructure only after Done. A Worker must not be copied.
type Worker struct {
	backend  Backend
	registry *Registry
	config   WorkerConfig
	schedule []Key
	cursor   atomic.Uint64
	active   atomic.Int64
	mu       sync.Mutex
	running  bool
	stopped  bool
	cancel   context.CancelFunc
	done     chan struct{}
}

func NewWorker(backend Backend, registry *Registry, config WorkerConfig) (*Worker, error) {
	if backend == nil || isNil(backend) || registry == nil || registry.entries == nil {
		return nil, fault.New(fault.Invalid, "worker requires a backend and registry")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	worker := &Worker{backend: backend, registry: registry, config: config.snapshot(), done: make(chan struct{})}
	for _, subscription := range config.Queues {
		key, err := NewKey(config.Namespace, subscription.Queue)
		if err != nil {
			return nil, err
		}
		for range subscription.Weight {
			worker.schedule = append(worker.schedule, key)
		}
	}
	return worker, nil
}

// Run satisfies foundation.Kernel. Parent context values are not passed to
// handlers; only the explicit observation recorder, cancellation and the envelope's captured attribution cross the
// boundary. Any backend failure stops admission and drains all owned handlers.
func (w *Worker) Run(ctx context.Context) error {
	if w == nil || w.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "worker requires initialization and a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	if w.running || w.stopped {
		w.mu.Unlock()
		return fault.New(fault.Closed, "worker is already running or stopped")
	}
	run, cancel := context.WithCancel(context.Background())
	run = observability.WithContext(run, observability.FromContext(ctx))
	w.running = true
	w.cancel = cancel
	w.mu.Unlock()
	stopParent := context.AfterFunc(ctx, w.stop)
	defer stopParent()
	var group sync.WaitGroup
	failures := make(chan error, w.config.Concurrency)
	for range w.config.Concurrency {
		group.Go(func() {
			if err := w.loop(run); err != nil {
				failures <- err
				w.stop()
			}
		})
	}
	group.Wait()
	cancel()
	w.mu.Lock()
	w.stopped = true
	w.running = false
	close(w.done)
	w.mu.Unlock()
	close(failures)
	var result error
	for err := range failures {
		result = errors.Join(result, err)
	}
	return result
}
func (w *Worker) loop(ctx context.Context) error {
	for ctx.Err() == nil {
		found, err := w.reserveOne(ctx)
		if err != nil {
			stopped := false
			failed := callback.Isolated("classify worker backend failure", func() error {
				stopped = !found && (errorgraph.Is(err, maintenance.ErrDraining) || ctx.Err() != nil && errorgraph.Is(err, ctx.Err()))
				return nil
			})
			if failed != nil {
				return failed
			}
			if stopped {
				return nil
			}
			return err
		}
		if !found {
			timer := time.NewTimer(w.config.PollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	}
	return nil
}
func (w *Worker) reserveOne(ctx context.Context) (bool, error) {
	for range len(w.schedule) {
		if err := observability.FromContext(ctx).Gate().Wait(ctx); err != nil {
			return false, err
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		key := w.schedule[(w.cursor.Add(1)-1)%uint64(len(w.schedule))]
		owner, err := lease.NewOwner()
		if err != nil {
			return false, err
		}
		operation, cancel := context.WithTimeout(ctx, w.config.OperationTimeout)
		var found value.Optional[Reservation]
		err = callback.Isolated("reserve job", func() error {
			var err error
			found, err = w.backend.JobReserve(operation, key, owner, w.config.LeaseDuration)
			return err
		})
		cancel()
		if err != nil {
			return false, err
		}
		reservation, ok := found.Get()
		if !ok {
			continue
		}
		w.active.Add(1)
		err = w.process(ctx, key, reservation)
		w.active.Add(-1)
		return true, err
	}
	return false, nil
}
func (w *Worker) stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	cancel := w.cancel
	if !w.running {
		close(w.done)
	}
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Stop rejects waiting from this worker's own active handler to avoid self-deadlock.
func (w *Worker) Stop(ctx context.Context) error {
	if w == nil || w.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "worker shutdown requires initialization and context")
	}
	frame, _ := ctx.Value(executionKey{}).(*executionFrame)
	if frame != nil && frame.worker == w && frame.active.Load() {
		return fault.New(fault.Cycle, "handler cannot wait for its own worker shutdown")
	}
	w.stop()
	select {
	case <-w.done:
		return nil
	default:
	}
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w *Worker) Done() <-chan struct{} {
	if w == nil {
		return nil
	}
	return w.done
}

// Active includes reserved, executing and finalizing operations, including handlers
// still running after cancellation. It never reports an abandoned goroutine as done.
func (w *Worker) Active() int64 {
	if w == nil {
		return 0
	}
	return w.active.Load()
}
