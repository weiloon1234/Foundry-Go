package hookqueries

import (
	"context"
	"errors"
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
			return record(ctx, tx, "saving", nil)
		},
		Creating: func(ctx context.Context, tx *database.Tx, draft *orm.Write[Member]) error {
			if err := record(ctx, tx, "creating", nil); err != nil {
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
			if err := record(ctx, tx, "updating", nil); err != nil {
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
			return record(ctx, tx, "deleting", nil)
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
			return record(ctx, tx, "saved", nil)
		},
		AfterCommit: func(ctx context.Context, operation lifecycle.Operation, changes orm.Changes[Member]) error {
			ctx = WithTrace(ctx, trace)
			if err := record(ctx, nil, "committed", nil); err != nil {
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
	return record(ctx, tx, name, nil)
}
