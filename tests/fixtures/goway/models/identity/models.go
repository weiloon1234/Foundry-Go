// Package identity supplies generated storage declarations with explicit auth identity adapters.
package identity

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Member is an auth subject. Example: Member{ID: id}.
//
//foundry:model table=identity_members var=Members key=ID identity=false
type Member struct{ ID model.ID[Member] }

// Other is a distinct reference owner. Example: Other{ID: id}.
//
//foundry:model table=identity_others var=Others key=ID identity=false
type Other struct{ ID model.ID[Other] }

// MemberIdentity retains the stored model and key. Example: MemberIdentity.ReferenceOf(member).
var MemberIdentity = orm.IdentityOf(Members.Table, Members.Key())

// OtherIdentity retains the distinct owner. Example: OtherIdentity.ReferenceOf(other).
var OtherIdentity = orm.IdentityOf(Others.Table, Others.Key())

// FoundryIdentity implements the auth subject contract without generated methods.
// Example: identity, err := member.FoundryIdentity().
func (m Member) FoundryIdentity() (model.Identity, error) {
	return MemberIdentity.ReferenceOf(m).Identity()
}

// FoundryIdentity implements the other subject's auth contract.
// Example: identity, err := other.FoundryIdentity().
func (m Other) FoundryIdentity() (model.Identity, error) {
	return OtherIdentity.ReferenceOf(m).Identity()
}
