package schedule

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func (s *Scheduler) execute(leader context.Context, index int, id ExecutionID, invocation Invocation, started time.Time) {
	d := s.registry.entries[index]
	base := observability.WithContext(context.Background(), observability.FromContext(leader))
	if attributed, err := attribution.WithContext(base, invocation.Origin); err == nil {
		base = attributed
	}
	observed, span, _ := observability.FromContext(leader).Start(base, observability.Operation{Kind: observability.Schedule, Name: observability.Name(d.id)})
	if observed != nil {
		base = observed
	}
	state, reason := Succeeded, NoReason
	var failure error
	defer func() {
		diagnostic := errordiag.Describe(failure)
		span.EndWithDiagnostic(observability.Result{Outcome: scheduleOutcome(state, reason)}, diagnostic)
		s.reportFinished(base, invocation, state, reason, diagnostic)
		// A completed or deliberately skipped occurrence is handled; a cancelled
		// one (stopping, leadership lost) may be replayed by catch-up.
		if state == Succeeded || state == Failed || state == Skipped {
			s.advanceCursor(index, invocation.IntendedAt)
		}
	}()
	invoke := func(owned context.Context) error {
		operation, unlink := contextlink.Link(base, leader, owned)
		defer unlink()
		// Error inspection can execute user-defined Is/Unwrap methods too. Keep
		// outcome classification inside owned callback isolation, and never feed a
		// domain error into the lease layer's coordination-error classification.
		err := callback.Isolated("schedule invocation outcome", func() error {
			state, reason, failure = s.invoke(operation, d, invocation)
			return nil
		})
		if err != nil {
			state, reason, failure = Failed, Panicked, err
		}
		return nil
	}
	if d.options.WithoutOverlap {
		// Timeout/leadership cancellation belongs to the callback context, not this
		// lease's parent. Renew until actual callback exit; do not deliberately
		// admit overlapping work while a known live callback ignores cancellation.
		ran, err := s.overlaps.With(context.Background(), overlapKey{s.config.Group, d.id}, d.options.OverlapTTL, 0, invoke)
		if !ran {
			state, reason = Skipped, OverlapBusy
			if err != nil {
				state, reason, failure = Failed, CoordinationFailed, err
			}
		} else if err != nil {
			failure = errors.Join(failure, err)
			lost := false
			inspection := callback.Isolated("classify schedule coordination", func() error {
				lost = errorgraph.Is(err, lease.ErrLost)
				return nil
			})
			switch {
			case inspection != nil:
				state, reason = Failed, CoordinationFailed
			case lost:
				state, reason = Failed, OverlapLost
			case state == Succeeded:
				state, reason = Failed, CoordinationFailed
			}
		}
	} else {
		_ = invoke(leader)
	}
	finished, clockErr := s.readClock()
	if clockErr != nil {
		finished = started
		state, reason = Failed, ClockFailed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	s.status[index].Active--
	for i := len(s.history) - 1; i >= 0; i-- {
		if s.history[i].Execution == id {
			s.history[i].State = state
			s.history[i].Reason = reason
			s.history[i].FinishedAt = finished
			return
		}
	}
	// Admission history may have been evicted while this long-running callback
	// retained its slot. Still retain its terminal result within the same bound.
	s.record(Record{Execution: id, Invocation: invocation, State: state, Reason: reason, StartedAt: started, FinishedAt: finished})
}

func (s *Scheduler) invoke(parent context.Context, d Declaration, invocation Invocation) (State, Reason, error) {
	ctx, err := attribution.WithContext(parent, invocation.Origin)
	if err != nil {
		return Failed, HandlerFailed, err
	}
	ctx, cancel := context.WithTimeout(ctx, d.options.Timeout)
	defer cancel()
	frame := &invocationFrame{scheduler: s, invocation: invocation}
	frame.active.Store(true)
	defer frame.active.Store(false)
	ctx = context.WithValue(ctx, invocationKey{}, frame)
	reason := NoReason
	call := func(label string, handler Handler) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if handler == nil {
			return nil
		}
		return callback.Isolated(label, func() error { return handler(ctx, invocation) })
	}
	if d.options.When != nil && ctx.Err() == nil {
		run := false
		err = callback.Isolated("schedule filter", func() error {
			var err error
			run, err = d.options.When(ctx, invocation)
			return err
		})
		if err == nil && !run && ctx.Err() == nil {
			return Skipped, Filtered, nil
		}
		if err != nil {
			reason = HookFailed
		}
	}
	if err == nil {
		if err = call("before schedule", d.options.Before); err != nil {
			reason = HookFailed
		}
	}
	if err == nil {
		err = call("schedule handler", d.handler)
		if err != nil {
			reason = HandlerFailed
		}
	}
	if err == nil {
		err = call("after schedule", d.options.After)
		if err != nil {
			reason = HookFailed
		}
	}
	err = errors.Join(err, ctx.Err())
	if err != nil && d.options.Failed != nil {
		failure := err
		hookErr := callback.Isolated("failed schedule", func() error { return d.options.Failed(ctx, invocation, failure) })
		if hookErr != nil && reason == NoReason {
			reason = HookFailed
		}
		err = errors.Join(err, hookErr)
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return Failed, TimedOut, err
	case parent.Err() != nil:
		if s.Snapshot().Stopping {
			return Cancelled, Stopped, err
		}
		return Cancelled, LeadershipLost, err
	case errorgraph.Is(err, fault.Panicked):
		return Failed, Panicked, err
	case err != nil:
		return Failed, reason, err
	default:
		return Succeeded, NoReason, nil
	}
}
