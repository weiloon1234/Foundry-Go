package hookqueries

import (
	"context"
	"errors"
	"runtime"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func ormMemberHooks(fields MemberTable) orm.Hooks[Member] {
	// State belongs to this one write. Saving and Saved must share the same
	// factory instance; separate writes must never share this variable.
	savingCalled := false
	var trace *Trace
	return orm.Hooks[Member]{
		Saving: func(ctx context.Context, tx *database.Tx, before value.Optional[Member], draft *orm.Write[Member]) error {
			trace, _ = ctx.Value(traceKey{}).(*Trace)
			if trace != nil && trace.Inspect != nil {
				if err := trace.Inspect(draft); err != nil {
					return err
				}
			}
			if savingCalled {
				return errors.New("factory reused across writes")
			}
			savingCalled = true
			return record(ctx, tx, "saving")
		},
		Creating: func(ctx context.Context, tx *database.Tx, draft *orm.Write[Member]) error {
			if err := record(ctx, tx, "creating"); err != nil {
				return err
			}
			if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil && trace.Recurse {
				_, err := fields.Insert(ctx, tx)
				return err
			}
			if !orm.WriteIsSet(draft, fields.Email.Set) {
				if err := orm.WriteSet(draft, fields.Email.Set(" DEFAULT@EXAMPLE.TEST ")); err != nil {
					return err
				}
			}
			if !orm.WriteIsSet(draft, fields.IntroducerID.Set) {
				introducer, err := fields.Query().Where(fields.IsIntroducer.Eq(true)).Require(ctx, tx)
				if err != nil {
					return err
				}
				if err := orm.WriteSet(draft, fields.IntroducerID.Set(introducer.ID)); err != nil {
					return err
				}
			}
			return nil
		},
		Updating: func(ctx context.Context, tx *database.Tx, before Member, draft *orm.Write[Member]) error {
			if err := record(ctx, tx, "updating"); err != nil {
				return err
			}
			// A hook may supply an otherwise empty patch using the locked model.
			if !orm.WriteIsSet(draft, fields.Key().Set) && !orm.WriteIsSet(draft, fields.Email.Set) && !orm.WriteIsSet(draft, fields.Nickname.Set) && !orm.WriteIsSet(draft, fields.IntroducerID.Set) && !orm.WriteIsSet(draft, fields.IsIntroducer.Set) && !orm.WriteIsSet(draft, fields.Rank.Set) {
				if err := orm.WriteSet(draft, fields.Rank.Set(before.Rank+1)); err != nil {
					return err
				}
			}
			return nil
		},
		Deleting: func(ctx context.Context, tx *database.Tx, before Member) error {
			trace, _ = ctx.Value(traceKey{}).(*Trace)
			return record(ctx, tx, "deleting")
		},
		Created: func(ctx context.Context, tx *database.Tx, changes orm.Changes[Member]) error {
			return recordORM(ctx, tx, "created", changes)
		},
		Updated: func(ctx context.Context, tx *database.Tx, changes orm.Changes[Member]) error {
			return recordORM(ctx, tx, "updated", changes)
		},
		Deleted: func(ctx context.Context, tx *database.Tx, changes orm.Changes[Member]) error {
			return recordORM(ctx, tx, "deleted", changes)
		},
		Saved: func(ctx context.Context, tx *database.Tx, changes orm.Changes[Member]) error {
			if !savingCalled {
				return errors.New("hook factory was recreated mid-write")
			}
			return record(ctx, tx, "saved")
		},
		AfterCommit: func(ctx context.Context, operation lifecycle.Operation, changes orm.Changes[Member]) error {
			ctx = WithTrace(ctx, trace)
			if err := record(ctx, nil, "committed"); err != nil {
				return err
			}
			if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil {
				trace.Operations = append(trace.Operations, operation)
				if trace.ORMAfterCommit != nil {
					return trace.ORMAfterCommit(ctx, operation, changes)
				}
			}
			return nil
		},
	}
}

func recordORM(ctx context.Context, tx *database.Tx, name string, changes orm.Changes[Member]) error {
	if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil {
		trace.ORMChanges = append(trace.ORMChanges, changes)
	}
	return record(ctx, tx, name)
}

func record(ctx context.Context, tx *database.Tx, name string) error {
	trace, _ := ctx.Value(traceKey{}).(*Trace)
	if trace != nil {
		trace.Calls = append(trace.Calls, name)
	}
	if tx != nil {
		if _, err := Logs.Insert(ctx, tx, Logs.Label.Set(name)); err != nil {
			return err
		}
	}
	if trace == nil || trace.FailAt != name {
		return nil
	}
	switch trace.Failure {
	case "panic":
		panic("private hook panic")
	case "goexit":
		runtime.Goexit()
	case "cancel":
		trace.Cancel()
		return nil
	}
	return Veto
}
