package observerqueries

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"

	"github.com/weiloon1234/Foundry-Go/value"
)

var recordFields RecordTable

func init() { recordFields = Records }

func recordHooks() orm.Hooks[Record] { return observerHooksFor("local", recordFields) }

// ORMObserverHooks preserves operation-local draft and change capture.
// Register only this adapter family when using Records; example: orm.NewObserver[Record]("first").Declare(func() orm.Hooks[Record] { return ORMObserverHooks("first") }).
func ORMObserverHooks(name string) orm.Hooks[Record] {
	return observerHooksFor(name, Records)
}

func observerHooksFor(name string, fields RecordTable) orm.Hooks[Record] {
	var trace *Trace
	savingCalled := false
	before := func(ctx context.Context, tx *database.Tx, stage string, draft *orm.Write[Record]) error {
		trace = traceOf(ctx)
		if err := record(ctx, tx, trace, name+"."+stage); err != nil {
			return err
		}
		current, set := orm.WriteGet(draft, func() func(arg0 string) orm.Assignment[Record] {
			receiver := fields.Name
			return func(arg0 string) orm.Assignment[Record] { return orm.Set(receiver, arg0) }
		}()).Get()
		if !set {
			current = "anonymous"
		}
		return orm.WriteSet(draft, orm.Set(fields.Name, current+"|"+name+"."+stage))
	}
	after := func(ctx context.Context, tx *database.Tx, stage string, changes orm.Changes[Record]) error {
		if trace != nil {
			trace.ORMChanges = append(trace.ORMChanges, changes)
		}
		return record(ctx, tx, trace, name+"."+stage)
	}
	return orm.Hooks[Record]{
		Saving: func(ctx context.Context, tx *database.Tx, _ value.Optional[Record], draft *orm.Write[Record]) error {
			if name == "local" {
				if trace := traceOf(ctx); trace != nil && trace.Inspect != nil {
					if err := trace.Inspect(draft); err != nil {
						return err
					}
				}
			}
			if savingCalled {
				return errors.New("observer factory reused across operations")
			}
			savingCalled = true
			return before(ctx, tx, "saving", draft)
		},
		Creating: func(ctx context.Context, tx *database.Tx, draft *orm.Write[Record]) error {
			return before(ctx, tx, "creating", draft)
		},
		Updating: func(ctx context.Context, tx *database.Tx, _ Record, draft *orm.Write[Record]) error {
			return before(ctx, tx, "updating", draft)
		},
		Deleting: func(ctx context.Context, tx *database.Tx, _ Record) error {
			trace = traceOf(ctx)
			return record(ctx, tx, trace, name+".deleting")
		},
		Created: func(ctx context.Context, tx *database.Tx, c orm.Changes[Record]) error {
			return after(ctx, tx, "created", c)
		},
		Updated: func(ctx context.Context, tx *database.Tx, c orm.Changes[Record]) error {
			return after(ctx, tx, "updated", c)
		},
		Deleted: func(ctx context.Context, tx *database.Tx, c orm.Changes[Record]) error {
			return after(ctx, tx, "deleted", c)
		},
		Saved: func(ctx context.Context, tx *database.Tx, c orm.Changes[Record]) error {
			if !savingCalled {
				return errors.New("observer recreated between saving and saved")
			}
			return after(ctx, tx, "saved", c)
		},
		AfterCommit: func(ctx context.Context, _ lifecycle.Operation, _ orm.Changes[Record]) error {
			return record(ctx, nil, trace, name+".committed")
		},
	}
}
