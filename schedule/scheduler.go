package schedule

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
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

// Scheduler borrows the shared lease manager. Run is single-use. Shutdown stops
// admission and cancels tasks, but Done waits for their actual exit. Keep the
// manager/backend alive until Done; Module supplies foundation dependency order.
// A Scheduler must not be copied.
type Scheduler struct {
	config                                     Config
	registry                                   *Registry
	namespace                                  keyspace.Namespace
	leadership                                 lease.Leases[Group]
	overlaps                                   lease.Leases[overlapKey]
	mu                                         sync.Mutex
	started, running, stopped, leader          bool
	cancel                                     context.CancelFunc
	done                                       chan struct{}
	active                                     int
	status                                     []ScheduleStatus
	history                                    []Record
	lastTime                                   time.Time
	cursor                                     int
	acquisitions, losses, coordinationFailures uint64
	tasks                                      sync.WaitGroup
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
	s := &Scheduler{config: config, registry: registry, namespace: manager.Namespace(), leadership: leader, overlaps: overlaps, done: make(chan struct{})}
	for _, d := range registry.entries {
		enabled := len(d.options.Environments) == 0 || slices.Contains(d.options.Environments, s.namespace.Environment)
		s.status = append(s.status, ScheduleStatus{ID: d.id, Enabled: enabled})
	}
	return s, nil
}

// Run drops caller context values; only cancellation is inherited. Tasks receive
// explicit system attribution. Leadership uncertainty cancels that epoch's tasks
// and retries election after a bounded polling interval, never executing unfenced.
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
	run = observability.WithContext(run, observability.FromContext(ctx))
	s.started = true
	s.running = true
	s.cancel = cancel
	s.mu.Unlock()
	stopParent := context.AfterFunc(ctx, s.stop)
	defer stopParent()
	var result error
	for run.Err() == nil {
		var fatal error
		_, err := s.leadership.WithProof(run, s.config.Group, s.config.LeadershipTTL, 0, func(leader context.Context, proof lease.Proof) error {
			fatal = s.epoch(leader, proof)
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
		if run.Err() != nil {
			break
		}
		if err != nil {
			s.mu.Lock()
			s.coordinationFailures++
			s.mu.Unlock()
		}
		timer := time.NewTimer(s.config.PollInterval)
		select {
		case <-run.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	s.stop()
	s.tasks.Wait()
	cancel()
	s.mu.Lock()
	s.running = false
	s.leader = false
	close(s.done)
	s.mu.Unlock()
	return result
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
func (s *Scheduler) epoch(ctx context.Context, proof lease.Proof) error {
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
		}
		next, err := d.spec.Next(start)
		if err != nil && !errors.Is(err, fault.Missing) {
			s.mu.Unlock()
			return err
		}
		s.status[i].Next = next
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
		if err := s.tick(ctx, proof, now); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
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

func (s *Scheduler) tick(ctx context.Context, proof lease.Proof, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	budget := s.config.MaxPerTick
	start := s.cursor
	s.cursor = (s.cursor + 1) % len(s.status)
	for offset := range len(s.status) {
		i := (start + offset) % len(s.status)
		status := &s.status[i]
		d := s.registry.entries[i]
		if !status.Enabled || status.Next.IsZero() || status.Next.After(now) {
			continue
		}
		if d.options.CatchUp.Window != 0 && status.Next.Before(now.Add(-d.options.CatchUp.Window)) {
			status.Next, _ = d.spec.Next(catchUpStart(now, d.options.CatchUp.Window))
		}
		maximum := max(1, d.options.CatchUp.Max)
		for considered := 0; !status.Next.IsZero() && !status.Next.After(now); considered++ {
			// Leave Next unchanged while paused. Resume uses the declaration's
			// existing catch-up window/budget instead of creating an unbounded burst.
			if observability.FromContext(ctx).Gate().Admit() != nil {
				return nil
			}
			if budget == 0 {
				return nil
			}
			if err := proof.Validate(); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			intended := status.Next
			invocation := occurrence(s.namespace, s.config.Group, d.id, intended)
			reason := NoReason
			stopBacklog := false
			switch {
			case considered >= maximum:
				reason = BacklogLimited
				stopBacklog = true
			case d.options.CatchUp.Window == 0 && intended.Before(now.Add(-s.config.Grace)):
				reason = Missed
				stopBacklog = true
			case s.active >= s.config.Concurrency:
				reason = CapacityReached
				stopBacklog = true
			case d.options.WithoutOverlap && status.Active != 0:
				reason = OverlapBusy
			}
			budget--
			if reason != NoReason {
				s.record(Record{Invocation: invocation, State: Skipped, Reason: reason, FinishedAt: now})
			} else {
				id, err := model.NewID[Execution]()
				if err != nil {
					return err
				}
				s.active++
				status.Active++
				s.record(Record{Execution: id, Invocation: invocation, State: Running, StartedAt: now})
				s.tasks.Go(func() { s.execute(ctx, i, id, invocation, now) })
			}
			cursor := intended
			if stopBacklog || d.options.CatchUp.Window == 0 {
				cursor = now
			}
			next, err := d.spec.Next(cursor)
			if err != nil && !errors.Is(err, fault.Missing) {
				return err
			}
			status.Next = next
			if stopBacklog {
				break
			}
		}
	}
	return nil
}

func (s *Scheduler) stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	cancel := s.cancel
	if !s.running {
		close(s.done)
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (s *Scheduler) Stop(ctx context.Context) error {
	if s == nil || s.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "scheduler stop requires initialization and context")
	}
	frame, _ := ctx.Value(invocationKey{}).(*invocationFrame)
	if frame != nil && frame.scheduler == s && frame.active.Load() {
		return fault.New(fault.Cycle, "schedule cannot wait for its own scheduler shutdown")
	}
	s.stop()
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
