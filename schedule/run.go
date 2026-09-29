package schedule

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// RunNow invokes one registered schedule immediately, outside leadership and
// its timing, for operators and tests (the `schedule test` command). The When
// predicate, hooks, timeout and WithoutOverlap protection apply as for a
// scheduled occurrence; calendar filters, catch-up cursors, history and the
// running scheduler's Concurrency do not. The occurrence is the current
// millisecond. The returned record classifies the outcome; the error reports
// only invalid use, never the handler's own failure.
func (s *Scheduler) RunNow(ctx context.Context, id ID) (Record, error) {
	if s == nil || s.registry == nil || ctx == nil {
		return Record{}, fault.New(fault.Invalid, "schedule run requires an initialized scheduler and context")
	}
	index := slices.IndexFunc(s.registry.entries, func(d Declaration) bool { return d.id == id })
	if index < 0 {
		return Record{}, fault.New(fault.Missing, "schedule is not registered")
	}
	if !s.status[index].Enabled {
		return Record{}, fault.New(fault.Invalid, "schedule is disabled in this environment")
	}
	started, err := s.readClock()
	if err != nil {
		return Record{}, err
	}
	execution, err := model.NewID[Execution]()
	if err != nil {
		return Record{}, err
	}
	d := s.registry.entries[index]
	invocation := occurrence(s.namespace, s.config.Group, d.id, started.Truncate(time.Millisecond))
	state, reason := Succeeded, NoReason
	run := func(owned context.Context) error {
		if err := callback.Isolated("schedule invocation outcome", func() error {
			state, reason, _ = s.invoke(owned, d, invocation)
			return nil
		}); err != nil {
			state, reason = Failed, Panicked
		}
		return nil
	}
	if d.options.WithoutOverlap {
		ran, err := s.overlaps.With(ctx, overlapKey{s.config.Group, d.id}, d.options.OverlapTTL, 0, run)
		switch {
		case !ran && err != nil:
			state, reason = Failed, CoordinationFailed
		case !ran:
			state, reason = Skipped, OverlapBusy
		case err != nil && state == Succeeded:
			state, reason = Failed, CoordinationFailed
		}
	} else {
		_ = run(ctx)
	}
	finished, err := s.readClock()
	if err != nil {
		finished = started
	}
	return Record{Execution: execution, Invocation: invocation, State: state, Reason: reason, StartedAt: started, FinishedAt: finished}, nil
}
