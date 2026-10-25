package softqueries

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func ormMemberHooks() orm.Hooks[Member] { return ORMHooks("local") }

// ORMHooks returns per-operation callbacks. Example: orm.NewObserver[Member]("soft.audit").Declare(func() orm.Hooks[Member] {return ORMHooks("observer")}).
func ORMHooks(owner string) orm.Hooks[Member] {
	var trace *Trace
	step := func(ctx context.Context, event string) error {
		trace, _ = ctx.Value(traceKey{}).(*Trace)
		if trace == nil {
			return nil
		}
		name := owner + "." + event
		trace.Steps = append(trace.Steps, name)
		if trace.CancelAt == name && trace.Cancel != nil {
			trace.Cancel()
		}
		if trace.VetoAt == name {
			return Veto
		}
		return nil
	}
	before := func(event string) func(context.Context, *database.Tx, Member) error {
		return func(ctx context.Context, _ *database.Tx, _ Member) error { return step(ctx, event) }
	}
	after := func(event string) func(context.Context, *database.Tx, orm.Changes[Member]) error {
		return func(ctx context.Context, _ *database.Tx, changes orm.Changes[Member]) error {
			if err := step(ctx, event); err != nil {
				return err
			}
			operation, present := changes.Operation().Get()
			if !present {
				return errors.New("captured model changes lost lifecycle operation")
			}
			if trace != nil {
				trace.ORMChanges = append(trace.ORMChanges, changes)
				trace.Operations = append(trace.Operations, operation)
			}
			return nil
		}
	}
	return orm.Hooks[Member]{
		Saving: func(ctx context.Context, _ *database.Tx, _ value.Optional[Member], _ *orm.Write[Member]) error {
			return step(ctx, "saving")
		},
		Creating: func(ctx context.Context, _ *database.Tx, _ *orm.Write[Member]) error { return step(ctx, "creating") },
		Updating: func(ctx context.Context, _ *database.Tx, _ Member, _ *orm.Write[Member]) error {
			return step(ctx, "updating")
		},
		Deleting:      before("deleting"),
		Restoring:     before("restoring"),
		ForceDeleting: before("force-deleting"),
		Created:       after("created"),
		Updated:       after("updated"),
		Deleted:       after("deleted"),
		Saved:         after("saved"),
		Restored:      after("restored"),
		ForceDeleted:  after("force-deleted"),
		AfterCommit: func(_ context.Context, operation lifecycle.Operation, changes orm.Changes[Member]) error {
			captured, present := changes.Operation().Get()
			if !present || captured != operation {
				return errors.New("after-commit operation differs from captured changes")
			}
			if trace != nil {
				trace.Committed = append(trace.Committed, operation)
			}
			return nil
		},
	}
}
