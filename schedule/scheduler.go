package schedule

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
)

var leadershipFamily = lease.Define[Group]("foundry.schedule.leadership", keyspace.StringKeys[Group]())

type overlapKey struct {
	group    Group
	schedule ID
}

var overlapFamily = lease.Define("foundry.schedule.overlap", keyspace.NewCodec(func(k overlapKey) (string, error) {
	// Semantic group/schedule IDs cannot contain the separator.
	return string(k.group) + ":" + string(k.schedule), nil
}))

// Scheduler borrows the shared lease manager. Run is single-use. Cancelling
// Run's context, or Drain, stops admission and lets running tasks finish within
// DrainTimeout while leadership is kept; Stop cancels tasks immediately. Done
// waits for their actual exit. Keep the manager/backend alive until Done;
// Module supplies foundation dependency order. A Scheduler must not be copied.
type Scheduler struct {
	config                                      Config
	registry                                    *Registry
	namespace                                   keyspace.Namespace
	leadership                                  lease.Leases[Group]
	overlaps                                    lease.Leases[overlapKey]
	cursors                                     CursorBackend
	logger                                      *slog.Logger
	mu                                          sync.Mutex
	started, running, stopped, draining, leader bool
	cancel, stopAdmission                       context.CancelFunc
	drainTimer                                  *time.Timer
	done                                        chan struct{}
	active                                      int
	status                                      []ScheduleStatus
	progress                                    []progress
	history                                     []Record
	lastTime                                    time.Time
	cursor                                      int
	acquisitions, losses, coordinationFailures  uint64
	tasks                                       sync.WaitGroup
}

func New(manager *lease.Manager, registry *Registry, config Config) (*Scheduler, error) {
	if manager == nil || registry == nil || len(registry.entries) == 0 {
		return nil, fault.New(fault.Invalid, "scheduler requires leases and registered schedules")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := manager.ValidateScope(config.LeadershipTTL, 0); err != nil {
		return nil, err
	}
	for _, d := range registry.entries {
		if err := manager.ValidateScope(d.options.OverlapTTL, 0); err != nil {
			return nil, err
		}
	}
	leader, err := leadershipFamily.Bind(manager)
	if err != nil {
		return nil, err
	}
	overlaps, err := overlapFamily.Bind(manager)
	if err != nil {
		return nil, err
	}
	s := &Scheduler{config: config, registry: registry, namespace: manager.Namespace(), leadership: leader, overlaps: overlaps, logger: config.Logger, done: make(chan struct{})}
	if cursors, ok := manager.BorrowedBackend().(CursorBackend); ok {
		s.cursors = cursors
	}
	for _, d := range registry.entries {
		enabled := len(d.options.Environments) == 0 || slices.Contains(d.options.Environments, s.namespace.Environment)
		s.status = append(s.status, ScheduleStatus{ID: d.id, Enabled: enabled})
	}
	s.progress = make([]progress, len(s.status))
	return s, nil
}

// bindLogger supplies the application logger before Run when Config.Logger is nil.
func (s *Scheduler) bindLogger(logger *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logger == nil && !s.started {
		s.logger = logger
	}
}

// Run drops caller context values; only cancellation is inherited. Tasks receive
// explicit system attribution. Leadership uncertainty cancels that epoch's tasks
// and retries election after a bounded polling interval, never executing unfenced.
// Parent cancellation starts a graceful drain.
func (s *Scheduler) Run(ctx context.Context) error {
	if s == nil || s.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "scheduler requires initialization and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return fault.New(fault.Closed, "scheduler already started or stopped")
	}
	run, cancel := context.WithCancel(context.Background())
	run = maintenance.WithContext(observability.WithContext(run, observability.FromContext(ctx)), admissionGate(ctx))
	admit, stopAdmission := context.WithCancel(run)
	s.started = true
	s.running = true
	s.cancel = cancel
	s.stopAdmission = stopAdmission
	s.mu.Unlock()
	stopParent := context.AfterFunc(ctx, s.drain)
	defer stopParent()
	var result error
	for admit.Err() == nil {
		var fatal error
		_, err := s.leadership.WithProof(run, s.config.Group, s.config.LeadershipTTL, 0, func(leader context.Context, proof lease.Proof) error {
			fatal = s.epoch(leader, admit, proof)
			if errorgraph.Is(fatal, context.Canceled) || errorgraph.Is(fatal, lease.ErrLost) {
				fatal = nil
			}
			if fatal != nil {
				s.stop()
			}
			return fatal
		})
		if fatal != nil {
			result = fatal
			break
		}
		if admit.Err() != nil {
			break
		}
		if err != nil {
			s.mu.Lock()
			s.coordinationFailures++
			s.mu.Unlock()
		}
		timer := time.NewTimer(s.config.PollInterval)
		select {
		case <-admit.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	s.stop()
	s.tasks.Wait()
	cancel()
	s.mu.Lock()
	if s.drainTimer != nil {
		s.drainTimer.Stop()
	}
	s.running = false
	s.leader = false
	close(s.done)
	s.mu.Unlock()
	return result
}

// admissionGate prefers the application's carried maintenance gate and falls
// back to the observation recorder's gate for standalone recorder contexts.
func admissionGate(ctx context.Context) *maintenance.Gate {
	if gate := maintenance.FromContext(ctx); gate != nil {
		return gate
	}
	return observability.FromContext(ctx).Gate()
}

func (s *Scheduler) readClock() (time.Time, error) {
	var now time.Time
	err := callback.Isolated("scheduler clock", func() error { now = s.config.Clock.Now().UTC(); return nil })
	if err != nil {
		return time.Time{}, err
	}
	if !validInstant(now) {
		return time.Time{}, fault.New(fault.Invalid, "scheduler clock is outside the supported range")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.Before(s.lastTime) {
		now = s.lastTime
	}
	s.lastTime = now
	return now, nil
}
func catchUpStart(now time.Time, window time.Duration) time.Time {
	start := now.Add(-window)
	epoch := time.Unix(0, 0).UTC()
	if !start.After(epoch) {
		return epoch
	}
	return start.Add(-time.Nanosecond)
}
func (s *Scheduler) epoch(ctx, admit context.Context, proof lease.Proof) error {
	// Persisted cursors resume catch-up after the last handled occurrence, so a
	// restart or leadership change does not replay completed work.
	cursors := s.loadCursors(ctx)
	now, err := s.readClock()
	if err != nil {
		return err
	}
	s.mu.Lock()
	for i, d := range s.registry.entries {
		if !s.status[i].Enabled {
			continue
		}
		start := now
		if d.options.CatchUp.Window != 0 {
			start = catchUpStart(now, d.options.CatchUp.Window)
			for _, handled := range []time.Time{s.progress[i].completed, cursors[i]} {
				if handled.After(start) {
					start = handled
				}
			}
		}
		next, err := d.spec.Next(start)
		if err != nil && !errors.Is(err, fault.Missing) {
			s.mu.Unlock()
			return err
		}
		s.status[i].Next = next
		s.progress[i].deferred = time.Time{}
	}
	s.leader = true
	s.acquisitions++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.leader = false
		if !s.stopped && ctx.Err() != nil {
			s.losses++
		}
		s.mu.Unlock()
	}()
	var ticks <-chan time.Time
	if s.config.Wake == nil {
		ticker := time.NewTicker(s.config.PollInterval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	for {
		if err := proof.Validate(); err != nil {
			return err
		}
		skipped, handled, err := s.tick(ctx, proof, now)
		s.reportSkipped(ctx, skipped)
		for _, item := range handled {
			s.advanceCursor(item.index, item.at)
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-admit.Done():
			return s.drainTasks(ctx)
		case _, ok := <-s.config.Wake:
			if !ok {
				return fault.New(fault.Closed, "scheduler wake channel closed")
			}
		case <-ticks:
		}
		now, err = s.readClock()
		if err != nil {
			return err
		}
	}
}

// drainTasks keeps leadership (and each task's overlap lease) while admitted
// tasks finish. The drain deadline cancels ctx through Stop.
func (s *Scheduler) drainTasks(ctx context.Context) error {
	finished := make(chan struct{})
	go func() { s.tasks.Wait(); close(finished) }()
	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type cursorAdvance struct {
	index int
	at    time.Time
}

func (s *Scheduler) tick(ctx context.Context, proof lease.Proof, now time.Time) ([]Record, []cursorAdvance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var skipped []Record
	var handled []cursorAdvance
	budget := s.config.MaxPerTick
	start := s.cursor
	s.cursor = (s.cursor + 1) % len(s.status)
	gate := maintenance.FromContext(ctx)
	for offset := range len(s.status) {
		i := (start + offset) % len(s.status)
		status := &s.status[i]
		pending := &s.progress[i]
		d := s.registry.entries[i]
		if !status.Enabled || status.Next.IsZero() || status.Next.After(now) {
			continue
		}
		catchUp := d.options.CatchUp.Window != 0
		due := []time.Time{status.Next}
		if catchUp {
			first := status.Next
			if first.Before(now.Add(-d.options.CatchUp.Window)) {
				first, _ = d.spec.Next(catchUpStart(now, d.options.CatchUp.Window))
			}
			recent, oldest, dropped, err := recentOccurrences(d.spec, first, now, max(1, d.options.CatchUp.Max))
			if err != nil {
				return skipped, handled, err
			}
			if dropped > 0 {
				record := Record{Invocation: occurrence(s.namespace, s.config.Group, d.id, oldest), State: Skipped, Reason: BacklogLimited, FinishedAt: now}
				s.record(record)
				skipped = append(skipped, record)
			}
			due = recent
			if len(due) == 0 {
				status.Next, _ = d.spec.Next(now)
				continue
			}
		}
	occurrences:
		for index, intended := range due {
			if budget == 0 {
				status.Next = intended
				return skipped, handled, nil
			}
			// Leave Next unchanged while paused. Resume uses the declaration's
			// existing catch-up window/budget instead of creating an unbounded burst.
			if !d.options.EvenInMaintenanceMode && gate.Admit() != nil {
				status.Next = intended
				break occurrences
			}
			if err := proof.Validate(); err != nil {
				return skipped, handled, err
			}
			if err := ctx.Err(); err != nil {
				return skipped, handled, err
			}
			if !d.options.admits(d.spec, intended) {
				// A filtered occurrence behaves like a time the spec never
				// produced: no history, no log and no execution slot.
				pending.deferred = time.Time{}
				if catchUp && index < len(due)-1 {
					continue
				}
				cursor := intended
				if !catchUp {
					cursor = now
				}
				next, err := d.spec.Next(cursor)
				if err != nil && !errors.Is(err, fault.Missing) {
					return skipped, handled, err
				}
				status.Next = next
				break occurrences
			}
			invocation := occurrence(s.namespace, s.config.Group, d.id, intended)
			reason := NoReason
			last := intended
			switch {
			case !catchUp && pending.deferred.IsZero() && intended.Before(now.Add(-s.config.Grace)):
				reason = Missed
			case s.active >= s.config.Concurrency:
				// Wait briefly for a slot before skipping the occurrence.
				if pending.deferred.IsZero() {
					pending.deferred = now
				}
				if now.Sub(pending.deferred) < s.config.CapacityWait {
					status.Next = intended
					break occurrences
				}
				reason = CapacityReached
				last = due[len(due)-1]
			case d.options.WithoutOverlap && status.Active != 0:
				reason = OverlapBusy
			}
			pending.deferred = time.Time{}
			budget--
			if reason != NoReason {
				record := Record{Invocation: invocation, State: Skipped, Reason: reason, FinishedAt: now}
				s.record(record)
				skipped = append(skipped, record)
				if catchUp {
					handled = append(handled, cursorAdvance{i, last})
				}
			} else {
				id, err := model.NewID[Execution]()
				if err != nil {
					return skipped, handled, err
				}
				s.active++
				status.Active++
				s.record(Record{Execution: id, Invocation: invocation, State: Running, StartedAt: now})
				s.tasks.Go(func() { s.execute(ctx, i, id, invocation, now) })
			}
			cursor := last
			if !catchUp {
				cursor = now
			}
			next, err := d.spec.Next(cursor)
			if err != nil && !errors.Is(err, fault.Missing) {
				return skipped, handled, err
			}
			status.Next = next
			if !catchUp || last != intended || index == len(due)-1 {
				break occurrences
			}
		}
	}
	return skipped, handled, nil
}

// recentScanLimit bounds one catch-up enumeration. A longer backlog narrows the
// scanned span toward now, so only the most recent occurrences are considered.
const recentScanLimit = 10000

// recentOccurrences returns at most limit of the most recent occurrences in
// [first, now], oldest first. dropped counts older occurrences that were not
// selected (a lower bound when the span had to be narrowed); oldest is the
// earliest of them.
func recentOccurrences(spec Spec, first, now time.Time, limit int) ([]time.Time, time.Time, int, error) {
	start := first
	for {
		var ring []time.Time
		var oldest time.Time
		dropped, scanned := 0, 0
		complete := true
		for at := start; !at.IsZero() && !at.After(now); {
			if scanned == recentScanLimit {
				complete = false
				break
			}
			if len(ring) == limit {
				if oldest.IsZero() {
					oldest = ring[0]
				}
				dropped++
				ring = ring[1:]
			}
			ring = append(ring, at)
			scanned++
			next, err := spec.Next(at)
			if err != nil && !errors.Is(err, fault.Missing) {
				return nil, time.Time{}, 0, err
			}
			at = next
		}
		if complete {
			if start.After(first) {
				oldest = first
				dropped = max(dropped, 1)
			}
			return ring, oldest, dropped, nil
		}
		span := now.Sub(start) / 2
		next, err := spec.Next(now.Add(-span).Add(-time.Nanosecond))
		if err != nil && !errors.Is(err, fault.Missing) {
			return nil, time.Time{}, 0, err
		}
		if next.IsZero() || !next.After(start) {
			return ring, first, max(dropped, 1), nil
		}
		start = next
	}
}

// drain stops admission and gives running tasks DrainTimeout to finish while
// leadership is kept. The deadline then cancels them like Stop.
func (s *Scheduler) drain() {
	s.mu.Lock()
	if s.stopped || s.draining {
		s.mu.Unlock()
		return
	}
	if !s.running || s.config.DrainTimeout == 0 {
		s.mu.Unlock()
		s.stop()
		return
	}
	s.draining = true
	stopAdmission := s.stopAdmission
	s.drainTimer = time.AfterFunc(s.config.DrainTimeout, s.stop)
	s.mu.Unlock()
	stopAdmission()
}
func (s *Scheduler) stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	cancel, stopAdmission := s.cancel, s.stopAdmission
	if !s.running {
		close(s.done)
	}
	s.mu.Unlock()
	if stopAdmission != nil {
		stopAdmission()
	}
	if cancel != nil {
		cancel()
	}
}

// Stop cancels running tasks immediately and bounds only the caller's wait.
func (s *Scheduler) Stop(ctx context.Context) error {
	return s.shutdown(ctx, s.stop)
}

// Drain stops admission and lets running tasks finish within DrainTimeout
// before cancelling them. ctx bounds only the caller's wait.
func (s *Scheduler) Drain(ctx context.Context) error {
	return s.shutdown(ctx, s.drain)
}

func (s *Scheduler) shutdown(ctx context.Context, begin func()) error {
	if s == nil || s.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "scheduler stop requires initialization and context")
	}
	frame, _ := ctx.Value(invocationKey{}).(*invocationFrame)
	if frame != nil && frame.scheduler == s && frame.active.Load() {
		return fault.New(fault.Cycle, "schedule cannot wait for its own scheduler shutdown")
	}
	begin()
	select {
	case <-s.done:
		return nil
	default:
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Scheduler) Done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.done
}
