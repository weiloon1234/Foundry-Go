package models

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// FoundryIdentity preserves the declared stored identity for authentication.
func (m User) FoundryIdentity() (model.Identity, error) {
	return orm.IdentityOf(Users.Table, Users.Key()).ReferenceOf(m).Identity()
}
