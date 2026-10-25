package recovering

import (
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberTable owns the stored recovery state. Example: MemberRecords.Email.Set(email).
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	Email         orm.Text[Member, string]
	EmailRevision orm.Column[Member, challenge.Revision[Member]]
	Password      orm.Column[Member, password.Hash]
	EmailVerified orm.Column[Member, bool]
	Enabled       orm.Column[Member, bool]
}

// MemberRecords keeps revision changes in the ordinary write lifecycle. Example: MemberRecords.Query().
var MemberRecords = declareMembers()

func declareMembers() MemberTable {
	var fields MemberTable
	fields = orm.Define("recovery_members", func(b orm.Builder[Member], m *Member) MemberTable {
		return MemberTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Email), orm.ColumnOf(b, &m.EmailRevision), orm.PasswordOf(b, &m.Password), orm.ColumnOf(b, &m.EmailVerified), orm.ColumnOf(b, &m.Enabled)}
	}, orm.WithHooks(func() orm.Hooks[Member] { return ormMemberHooks(fields) }))
	return fields
}

// MemberIdentity preserves the persisted model and key identity. Example: MemberIdentity.ReferenceOf(member).
var MemberIdentity = orm.IdentityOf(MemberRecords.Table, MemberRecords.Key())
