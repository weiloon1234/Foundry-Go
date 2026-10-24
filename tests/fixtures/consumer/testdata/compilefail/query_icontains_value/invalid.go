package invalid

import (
	"foundry.test/consumer/reporting"
)

var invalid = reporting.MemberModels.Name.IContains(42)
