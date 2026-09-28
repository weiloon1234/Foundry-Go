package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var invalid = datatable.Spec[reporting.Member, reporting.MemberRow, reporting.Authority]{Row: reporting.OrderRowJSON()}
