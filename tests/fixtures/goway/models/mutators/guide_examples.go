package mutators

import (
	"foundry.test/goway/coremodels"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// doc-example:begin input-assignment
func emailInput(input coremodels.EmailInput) orm.Assignment[Member] {
	return orm.Set(Members.Email, input)
}

// doc-example:end input-assignment

// doc-example:begin read-accessor
func emailLink(member Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}

// doc-example:end read-accessor
