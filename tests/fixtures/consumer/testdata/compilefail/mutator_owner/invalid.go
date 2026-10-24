package invalid

import (
	"foundry.test/consumer/linkqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.InputText[linkqueries.Member, string, string] = mutatorqueries.Members.Email
