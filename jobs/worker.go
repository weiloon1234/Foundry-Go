package jobs

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"reflect"
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

// Worker owns reservation loops and their handler lifetimes. Run is single-use.
// Cancelling Run's context, or calling Drain, stops new reservations while
// admitted handlers keep their heartbeats for up to DrainTimeout; Stop cancels
// admitted work immediately. Both bound only the caller's wait: Done closes only
// when every owned handler has actually returned, including context-ignoring
// ones. Close borrowed infrastructure only after Done. A Worker must not be copied.
type Worker struct {
	backend       Backend
	registry      *Registry
	config        WorkerConfig
	logger        *slog.Logger
	schedule      []Key
	queues        []Key
	cursor        atomic.Uint64
	active        atomic.Int64
	mu            sync.Mutex
	running       bool
	draining      bool
	stopped       bool
	cancel        context.CancelFunc
	stopReserving context.CancelFunc
	drainTimer    *time.Timer
	done          chan struct{}
	sinks         []FailureSink
}

// workerRun separates the two shutdown phases. Reservations use reserve, which
// ends when draining begins; admitted handlers use work, which ends only on a
// hard stop or when the drain deadline expires.
type workerRun struct {
	work    context.Context
	reserve context.Context
}

func NewWorker(backend Backend, registry *Registry, config WorkerConfig, options ...WorkerOption) (*Worker, error) {
	if backend == nil || isNil(backend) || registry == nil || registry.entries == nil {
		return nil, fault.New(fault.Invalid, "worker requires a backend and registry")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	worker := &Worker{backend: backend, registry: registry, config: config.snapshot(), done: make(chan struct{})}
	for _, option := range options {
		if option == nil {
			return nil, fault.New(fault.Invalid, "nil job worker option")
		}
		if err := option(worker); err != nil {
			return nil, err
		}
	}
	for _, subscription := range config.Queues {
		key, err := NewKey(config.Namespace, subscription.Queue)
		if err != nil {
			return nil, err
		}
		worker.queues = append(worker.queues, key)
		for range subscription.Weight {
			worker.schedule = append(worker.schedule, key)
		}
	}
	return worker, nil
}

// Run satisfies foundation.Kernel. Parent context values are not passed to
// handlers; only the explicit observation recorder, cancellation and the
// envelope's captured attribution cross the boundary. Parent cancellation starts
// a graceful drain. Backend failures are logged and retried with jittered
// backoff; they never stop the worker or abandon an admitted handler.
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
	work, cancel := context.WithCancel(context.Background())
	work = maintenance.WithContext(observability.WithContext(work, observability.FromContext(ctx)), admissionGate(ctx))
	reserve, stopReserving := context.WithCancel(work)
	w.running = true
	w.cancel = cancel
	w.stopReserving = stopReserving
	w.mu.Unlock()
	stopParent := context.AfterFunc(ctx, w.drain)
	defer stopParent()
	run := workerRun{work: work, reserve: reserve}
	var group sync.WaitGroup
	for range w.config.Concurrency {
		group.Go(func() { w.loop(run) })
	}
	group.Wait()
	stopReserving()
	cancel()
	w.mu.Lock()
	if w.drainTimer != nil {
		w.drainTimer.Stop()
	}
	w.stopped = true
	w.running = false
	close(w.done)
	w.mu.Unlock()
	return nil
}

func (w *Worker) loop(run workerRun) {
	idle, failures := w.config.PollInterval, 0
	for run.reserve.Err() == nil {
		// Subscribe before reserving so an enqueue racing with an empty
		// reservation still wakes this loop instead of waiting a full interval.
		wake := w.wakeup()
		key, reservation, reservedAt, found, err := w.reserveNext(run)
		if err != nil {
			if w.shuttingDown(run.reserve, err) {
				return
			}
			failures++
			w.logBackendFailure(run.work, "reserve", key, err)
			if !pause(run.reserve, w.failureDelay(failures), nil) {
				return
			}
			continue
		}
		if !found {
			failures = 0
			if !pause(run.reserve, idle, wake) {
				return
			}
			idle = w.nextIdle(idle)
			continue
		}
		idle = w.config.PollInterval
		w.active.Add(1)
		healthy := w.process(run, key, reservation, reservedAt)
		w.active.Add(-1)
		if healthy {
			failures = 0
			continue
		}
		failures++
		if !pause(run.reserve, w.failureDelay(failures), nil) {
			return
		}
	}
}

// reserveNext tries each distinct subscribed queue at most once per cycle. The
// weighted cursor selects which queue is tried first, so weights shape service
// without repeating backend calls for the same idle queue.
func (w *Worker) reserveNext(run workerRun) (Key, Reservation, time.Time, bool, error) {
	first := w.schedule[(w.cursor.Add(1)-1)%uint64(len(w.schedule))]
	order := make([]Key, 0, len(w.queues))
	order = append(order, first)
	for _, key := range w.queues {
		if key != first {
			order = append(order, key)
		}
	}
	for _, key := range order {
		if err := maintenance.FromContext(run.reserve).Wait(run.reserve); err != nil {
			return key, Reservation{}, time.Time{}, false, err
		}
		if err := run.reserve.Err(); err != nil {
			return key, Reservation{}, time.Time{}, false, err
		}
		owner, err := lease.NewOwner()
		if err != nil {
			return key, Reservation{}, time.Time{}, false, err
		}
		// The reservation itself is not interrupted by a drain: a reply that
		// arrives after draining began is released rather than abandoned.
		operation, cancel := context.WithTimeout(run.work, w.config.OperationTimeout)
		reservedAt := time.Now()
		var found value.Optional[Reservation]
		err = callback.Isolated("reserve job", func() error {
			var err error
			found, err = w.backend.JobReserve(operation, key, owner, w.config.LeaseDuration)
			return err
		})
		cancel()
		if err != nil {
			return key, Reservation{}, time.Time{}, false, err
		}
		if reservation, ok := found.Get(); ok {
			return key, reservation, reservedAt, true, nil
		}
	}
	return Key{}, Reservation{}, time.Time{}, false, nil
}

// admissionGate prefers the application's carried maintenance gate and falls
// back to the observation recorder's gate for standalone recorder contexts.
func admissionGate(ctx context.Context) *maintenance.Gate {
	if gate := maintenance.FromContext(ctx); gate != nil {
		return gate
	}
	return observability.FromContext(ctx).Gate()
}

// shuttingDown distinguishes the worker's own drain/stop from a backend
// failure. Error inspection runs custom methods, so it stays isolated.
func (w *Worker) shuttingDown(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	draining := false
	if failed := callback.Isolated("classify worker backend failure", func() error {
		draining = errorgraph.Is(err, maintenance.ErrDraining)
		return nil
	}); failed != nil {
		return false
	}
	return draining
}

// wakeup subscribes to local work in each of this worker's queues.
func (w *Worker) wakeup() []<-chan struct{} {
	source, ok := w.backend.(WakeBackend)
	if !ok {
		return nil
	}
	var wakes []<-chan struct{}
	if callback.Isolated("subscribe job wakeup", func() error {
		for _, key := range w.queues {
			if wake := source.JobWakeup(key); wake != nil {
				wakes = append(wakes, wake)
			}
		}
		return nil
	}) != nil {
		return nil
	}
	return wakes
}

func (w *Worker) nextIdle(idle time.Duration) time.Duration {
	if w.config.MaxPollInterval <= w.config.PollInterval {
		return w.config.PollInterval
	}
	return min(idle*2, w.config.MaxPollInterval)
}

// failureDelay grows exponentially from PollInterval up to FailureBackoff and
// selects uniformly from its upper half, so replicas do not retry in lockstep.
func (w *Worker) failureDelay(failures int) time.Duration {
	ceiling := max(w.config.FailureBackoff, w.config.PollInterval)
	delay := w.config.PollInterval
	for i := 1; i < failures && delay < ceiling; i++ {
		delay *= 2
	}
	delay = min(delay, ceiling)
	half := delay / 2
	return half + rand.N(delay-half+1)
}

// pause waits for delay, any wake signal or ctx. It reports false when ctx
// ended, meaning the caller must stop reserving.
func pause(ctx context.Context, delay time.Duration, wakes []<-chan struct{}) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	if len(wakes) <= 1 {
		var wake <-chan struct{}
		if len(wakes) == 1 {
			wake = wakes[0]
		}
		select {
		case <-ctx.Done():
			return false
		case <-wake:
		case <-timer.C:
		}
		return ctx.Err() == nil
	}
	cases := make([]reflect.SelectCase, 0, len(wakes)+2)
	cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())}, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(timer.C)})
	for _, wake := range wakes {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(wake)})
	}
	chosen, _, _ := reflect.Select(cases)
	return chosen != 0 && ctx.Err() == nil
}

// drain stops reservations and gives admitted handlers DrainTimeout to finish
// while their heartbeats continue. The deadline then cancels them like Stop.
func (w *Worker) drain() {
	w.mu.Lock()
	if w.stopped || w.draining {
		w.mu.Unlock()
		return
	}
	if !w.running || w.config.DrainTimeout == 0 {
		w.mu.Unlock()
		w.stop()
		return
	}
	w.draining = true
	stopReserving := w.stopReserving
	w.drainTimer = time.AfterFunc(w.config.DrainTimeout, w.stop)
	w.mu.Unlock()
	stopReserving()
}
func (w *Worker) stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	cancel, stopReserving := w.cancel, w.stopReserving
	if !w.running {
		close(w.done)
	}
	w.mu.Unlock()
	if stopReserving != nil {
		stopReserving()
	}
	if cancel != nil {
		cancel()
	}
}

// Stop cancels admitted work immediately. Interrupted attempts are released
// without consuming their retry budget on built-in backends. Stop rejects waiting
// from this worker's own active handler to avoid self-deadlock.
func (w *Worker) Stop(ctx context.Context) error {
	return w.shutdown(ctx, w.stop)
}

// Drain stops new reservations and lets admitted handlers finish within
// DrainTimeout before cancelling them. ctx bounds only the caller's wait.
func (w *Worker) Drain(ctx context.Context) error {
	return w.shutdown(ctx, w.drain)
}

func (w *Worker) shutdown(ctx context.Context, begin func()) error {
	if w == nil || w.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "worker shutdown requires initialization and context")
	}
	frame, _ := ctx.Value(executionKey{}).(*executionFrame)
	if frame != nil && frame.worker == w && frame.active.Load() {
		return fault.New(fault.Cycle, "handler cannot wait for its own worker shutdown")
	}
	begin()
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
