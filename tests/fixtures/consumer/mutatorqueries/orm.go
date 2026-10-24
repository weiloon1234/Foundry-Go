package mutatorqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberTable keeps write inputs separate from stored predicate values. Example: Members.Email.Set(" ADA@EXAMPLE.TEST ").
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	// Email is the delivery address. ChangeOf compares stored values after MutateEmail.
	Email    orm.InputText[Member, string, string]
	Nickname orm.NullableInputText[Member, string, string]
	Attempts orm.InputNumber[Member, int, int]
	State    orm.InputEnum[Member, State, State]
}

// Members binds the existing named mutators; accessors remain explicit model calls.
// Example: stored, err := Members.Require(ctx,db,id); display, err := stored.AccessEmail().
var Members = orm.Define("mutator_members", func(b orm.Builder[Member], m *Member) MemberTable {
	return MemberTable{orm.TableOf(b, &m.ID), orm.InputTextOf[Member, string](b, &m.Email, nil), orm.NullableInputTextOf[Member, string](b, &m.Nickname, nil), orm.InputNumberOf[Member, int](b, &m.Attempts, nil), orm.InputEnumOf[Member, State](b, &m.State, nil)}
})

// MemberIdentity uses the stored model-owned key, independently of AccessID.
// Example: MemberIdentity.ReferenceOf(member).
var MemberIdentity = orm.IdentityOf(Members.Table, Members.Key())
