package pivothooks

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Veto is returned by a hook named in Trace.VetoAt.
var Veto = errors.New("pivot hook veto")

// Event records one hook invocation with the pivot state it observed.
type Event struct {
	Name   string
	Before value.Optional[Assignment]
	After  value.Optional[Assignment]
}

// Trace collects hook events for one operation through its context.
type Trace struct {
	Events    []Event
	Committed []lifecycle.Operation
	// VetoAt fails the Nth (1-based) invocation of the named hook.
	VetoAt    string
	VetoCount int
	// OnCreated runs inside the local created hook, for nested writes and
	// hook failures.
	OnCreated func(context.Context, *database.Tx) error
	seen      map[string]int
}

type traceKey struct{}

// WithTrace attaches a trace to the operation context.
func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}

func (t *Trace) record(event Event) error {
	t.Events = append(t.Events, event)
	if t.seen == nil {
		t.seen = make(map[string]int)
	}
	t.seen[event.Name]++
	if t.VetoAt == event.Name && t.seen[event.Name] == max(t.VetoCount, 1) {
		return Veto
	}
	return nil
}

func assignmentHooks() AssignmentHooks { return Hooks("local") }

// Hooks records every lifecycle step for the owner (local or provider).
func Hooks(owner string) AssignmentHooks {
	record := func(ctx context.Context, name string, before, after value.Optional[Assignment]) error {
		trace, _ := ctx.Value(traceKey{}).(*Trace)
		if trace == nil {
			return nil
		}
		return trace.record(Event{Name: owner + "." + name, Before: before, After: after})
	}
	current := func(name string) func(context.Context, *database.Tx, Assignment) error {
		return func(ctx context.Context, _ *database.Tx, stored Assignment) error {
			return record(ctx, name, value.Set(stored), value.Optional[Assignment]{})
		}
	}
	changed := func(name string) func(context.Context, *database.Tx, AssignmentChanges) error {
		return func(ctx context.Context, _ *database.Tx, changes AssignmentChanges) error {
			return record(ctx, name, changes.Before(), changes.After())
		}
	}
	return AssignmentHooks{
		Creating: func(ctx context.Context, _ *database.Tx, _ *AssignmentDraft) error {
			return record(ctx, "creating", value.Optional[Assignment]{}, value.Optional[Assignment]{})
		},
		Updating: func(ctx context.Context, _ *database.Tx, stored Assignment, _ *AssignmentDraft) error {
			return record(ctx, "updating", value.Set(stored), value.Optional[Assignment]{})
		},
		Deleting: current("deleting"),
		Created: func(ctx context.Context, tx *database.Tx, changes AssignmentChanges) error {
			if err := changed("created")(ctx, tx, changes); err != nil {
				return err
			}
			if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil && trace.OnCreated != nil && owner == "local" {
				return trace.OnCreated(ctx, tx)
			}
			return nil
		},
		Updated: changed("updated"),
		Deleted: changed("deleted"),
		AfterCommit: func(ctx context.Context, operation lifecycle.Operation, _ AssignmentChanges) error {
			if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil && owner == "local" {
				trace.Committed = append(trace.Committed, operation)
			}
			return nil
		},
	}
}
