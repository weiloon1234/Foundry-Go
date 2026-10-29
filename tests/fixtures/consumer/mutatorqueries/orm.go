package mutatorqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberTable keeps write inputs separate from stored predicate values. Example: orm.InputTextSet(Members.Email, " ADA@EXAMPLE.TEST ").
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	// Email is the delivery address. ChangeOf compares stored values after MutateEmail.
	// Custom getter AccessEmail is explicit; predicates compare stored values.
	// Custom setter MutateEmail runs on write, not during assignment construction.
	Email    orm.InputText[Member, string, string]
	Nickname orm.NullableInputText[Member, string, string]
	Attempts orm.InputNumber[Member, int, int]
	State    orm.InputEnum[Member, State, State]
}

// Members binds the existing named mutators; accessors remain explicit model calls.
// Example: stored, err := orm.RequireKey(ctx, db, Members.Table,id); display, err := stored.AccessEmail().
var Members = orm.Define("mutator_members", func(b orm.Builder[Member], m *Member) MemberTable {
	return MemberTable{orm.TableOf(b, &m.ID), orm.InputTextOf[Member, string](b, &m.Email, nil), orm.NullableInputTextOf[Member, string](b, &m.Nickname, nil), orm.InputNumberOf[Member, int](b, &m.Attempts, nil), orm.InputEnumOf[Member, State](b, &m.State, nil)}
}, orm.WithHooks(func() orm.Hooks[Member] {
	return orm.Hooks[Member]{Creating: func(ctx context.Context, _ *database.Tx, w *orm.Write[Member]) error {
		if inspect, _ := ctx.Value(draftInspectionKey{}).(func(*orm.Write[Member]) error); inspect != nil {
			return inspect(w)
		}
		return nil
	}}
}))

type draftInspectionKey struct{}

// WithDraftInspection observes fresh mutation inputs in this operation's before hook.
// Example: WithDraftInspection(ctx, func(w *orm.Write[Member]) error { return nil }).
func WithDraftInspection(ctx context.Context, inspect func(*orm.Write[Member]) error) context.Context {
	return context.WithValue(ctx, draftInspectionKey{}, inspect)
}

// MemberIdentity uses the stored model-owned key, independently of AccessID.
// Example: MemberIdentity.ReferenceOf(member).
var MemberIdentity = orm.IdentityOf(Members.Table, Members.Key())
