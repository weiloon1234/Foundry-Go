package invalid

import (
	"foundry.test/consumer/reporting"
)

var invalid = reporting.MemberFields().Name.IContains(42)
