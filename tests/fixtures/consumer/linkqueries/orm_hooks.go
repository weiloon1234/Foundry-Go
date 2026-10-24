package linkqueries

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ORMHooks constructs a per-write factory result for either local or provider callbacks.
// Example: orm.NewObserver[Membership]("provider.link").Declare(func() orm.Hooks[Membership] { return ORMHooks("provider") }).
func ORMHooks(owner string) orm.Hooks[Membership] {
	return membershipORMHooks(owner, Memberships)
}

func membershipORMHooks(owner string, fields MembershipTable) orm.Hooks[Membership] {
	before, committed := membershipOperation(owner)
	after := func(ctx context.Context, name string, changes orm.Changes[Membership]) error {
		if trace := traceFrom(ctx); trace != nil {
			trace.ORMChanges = append(trace.ORMChanges, changes)
		}
		return before(ctx, name)
	}
	return orm.Hooks[Membership]{
		Saving: func(ctx context.Context, _ *database.Tx, prior value.Optional[Membership], draft *orm.Write[Membership]) error {
			if !prior.IsSet() && (!orm.WriteIsSet(draft, fields.MemberID.Set) || !orm.WriteIsSet(draft, fields.GroupCode.Set)) {
				return errors.New("pivot hooks did not receive derived keys")
			}
			return before(ctx, "saving")
		},
		Creating: func(ctx context.Context, _ *database.Tx, _ *orm.Write[Membership]) error {
			return before(ctx, "creating")
		},
		Deleting:      func(ctx context.Context, _ *database.Tx, _ Membership) error { return before(ctx, "deleting") },
		Restoring:     func(ctx context.Context, _ *database.Tx, _ Membership) error { return before(ctx, "restoring") },
		ForceDeleting: func(ctx context.Context, _ *database.Tx, _ Membership) error { return before(ctx, "force-deleting") },
		Created: func(ctx context.Context, _ *database.Tx, c orm.Changes[Membership]) error {
			return after(ctx, "created", c)
		},
		Saved: func(ctx context.Context, _ *database.Tx, c orm.Changes[Membership]) error {
			return after(ctx, "saved", c)
		},
		Deleted: func(ctx context.Context, _ *database.Tx, c orm.Changes[Membership]) error {
			return after(ctx, "deleted", c)
		},
		Restored: func(ctx context.Context, _ *database.Tx, c orm.Changes[Membership]) error {
			return after(ctx, "restored", c)
		},
		ForceDeleted: func(ctx context.Context, _ *database.Tx, c orm.Changes[Membership]) error {
			return after(ctx, "force-deleted", c)
		},
		AfterCommit: func(_ context.Context, op lifecycle.Operation, _ orm.Changes[Membership]) error {
			committed(op)
			return nil
		},
	}
}
