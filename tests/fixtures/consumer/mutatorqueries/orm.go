package mutatorqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type draftInspectionKey struct{}

// WithDraftInspection observes fresh mutation inputs in this operation's before hook.
// Example: WithDraftInspection(ctx, func(w *orm.Write[Member]) error { return nil }).
func WithDraftInspection(ctx context.Context, inspect func(*orm.Write[Member]) error) context.Context {
	return context.WithValue(ctx, draftInspectionKey{}, inspect)
}

// MemberIdentity uses the stored model-owned key, independently of AccessID.
// Example: MemberIdentity.ReferenceOf(member).
var MemberIdentity = orm.IdentityOf(Members.Table, Members.Key())

func memberHooks() orm.Hooks[Member] {
	return orm.Hooks[Member]{Creating: func(ctx context.Context, _ *database.Tx, w *orm.Write[Member]) error {
		if inspect, _ := ctx.Value(draftInspectionKey{}).(func(*orm.Write[Member]) error); inspect != nil {
			return inspect(w)
		}
		return nil
	}}
}
