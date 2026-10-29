// Package identity supplies handwritten storage declarations with explicit auth identity adapters.
package identity

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Member is an auth subject. Example: Member{ID: id}.
type Member struct{ ID model.ID[Member] }

// Other is a distinct reference owner. Example: Other{ID: id}.
type Other struct{ ID model.ID[Other] }

// Members declares the stored member key. Example: orm.Find(ctx, db, Members.Table, id).
var Members = orm.Define("identity_members", func(b orm.Builder[Member], m *Member) orm.Table[Member, model.ID[Member]] {
	return orm.TableOf(b, &m.ID)
})

// Others declares the other owner's key. Example: orm.Find(ctx, db, Others.Table, id).
var Others = orm.Define("identity_others", func(b orm.Builder[Other], m *Other) orm.Table[Other, model.ID[Other]] {
	return orm.TableOf(b, &m.ID)
})

// MemberIdentity retains the stored model and key. Example: MemberIdentity.ReferenceOf(member).
var MemberIdentity = orm.IdentityOf(Members, Members.Key())

// OtherIdentity retains the distinct owner. Example: OtherIdentity.ReferenceOf(other).
var OtherIdentity = orm.IdentityOf(Others, Others.Key())

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
