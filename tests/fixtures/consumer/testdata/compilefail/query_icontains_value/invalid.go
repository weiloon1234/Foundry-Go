package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/reporting"
)

var invalid = orm.IContains(reporting.MemberModels.Name, 42)
