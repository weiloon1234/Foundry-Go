package pivothooks

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ORMHooks uses the same trace and veto owner as model writes and provider observers.
func ORMHooks(owner string) orm.Hooks[Assignment] {
	record := assignmentEvents(owner)
	changed := func(name string) func(context.Context, *database.Tx, orm.Changes[Assignment]) error {
		return func(ctx context.Context, _ *database.Tx, c orm.Changes[Assignment]) error {
			return record(ctx, name, c.Before(), c.After())
		}
	}
	return orm.Hooks[Assignment]{
		Creating: func(ctx context.Context, tx *database.Tx, _ *orm.Write[Assignment]) error {
			return record(ctx, "creating", value.Optional[Assignment]{}, value.Optional[Assignment]{})
		},
		Updating: func(ctx context.Context, tx *database.Tx, old Assignment, _ *orm.Write[Assignment]) error {
			return record(ctx, "updating", value.Set(old), value.Optional[Assignment]{})
		},
		Deleting: func(ctx context.Context, _ *database.Tx, old Assignment) error {
			return record(ctx, "deleting", value.Set(old), value.Optional[Assignment]{})
		},
		Created: func(ctx context.Context, tx *database.Tx, c orm.Changes[Assignment]) error {
			if err := changed("created")(ctx, tx, c); err != nil {
				return err
			}
			return assignmentCreated(ctx, tx, owner)
		},
		Updated: changed("updated"), Deleted: changed("deleted"),
		AfterCommit: func(ctx context.Context, op lifecycle.Operation, _ orm.Changes[Assignment]) error {
			return assignmentCommitted(ctx, owner, op)
		},
	}
}

func assignmentCreated(ctx context.Context, tx *database.Tx, owner string) error {
	if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil && trace.OnCreated != nil && owner == "local" {
		return trace.OnCreated(ctx, tx)
	}
	return nil
}
func assignmentCommitted(ctx context.Context, owner string, op lifecycle.Operation) error {
	if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil && owner == "local" {
		trace.Committed = append(trace.Committed, op)
	}
	return nil
}
