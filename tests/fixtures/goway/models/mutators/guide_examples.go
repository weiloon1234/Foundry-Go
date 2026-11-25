package mutators

import (
	"foundry.test/goway/coremodels"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// doc-example:begin inputs-after
func emailInput(input coremodels.EmailInput) orm.Assignment[Member] {
	return orm.Set(Members.Email, input)
}

// doc-example:end inputs-after

// doc-example:begin accessors-after
func emailLink(member Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}

// doc-example:end accessors-after
