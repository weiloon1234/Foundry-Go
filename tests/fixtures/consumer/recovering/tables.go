package recovering

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

var memberFields MemberTable

func init() { memberFields = MemberRecords }

func memberHooks() orm.Hooks[Member] { return ormMemberHooks(memberFields) }

// MemberIdentity preserves the persisted model and key identity. Example: MemberIdentity.ReferenceOf(member).
var MemberIdentity = orm.IdentityOf(MemberRecords.Table, MemberRecords.Key())

// FoundryIdentity preserves stored attribution for authentication and recovery.
func (m Member) FoundryIdentity() (model.Identity, error) {
	return MemberIdentity.ReferenceOf(m).Identity()
}
