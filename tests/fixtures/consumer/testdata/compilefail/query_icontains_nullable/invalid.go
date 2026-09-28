package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = reporting.MemberFields().Nickname.IContains(value.Of("name"))
