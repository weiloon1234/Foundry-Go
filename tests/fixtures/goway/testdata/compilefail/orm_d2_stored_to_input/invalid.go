package invalid

import (
	"foundry.test/goway/coremodels"
	"foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.InputTextSet(mutators.Members.Email, coremodels.StoredEmail("stored"))
