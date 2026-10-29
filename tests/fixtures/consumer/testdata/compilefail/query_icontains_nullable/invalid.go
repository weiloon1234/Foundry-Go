package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = orm.NullableTextIContains(reporting.MemberModels.Nickname, value.Of("name"))
