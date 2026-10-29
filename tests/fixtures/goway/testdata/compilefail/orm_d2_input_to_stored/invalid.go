package invalid

import (
	"foundry.test/goway/coremodels"
	"foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.InputTextEq(mutators.Members.Email, coremodels.EmailInput{})
