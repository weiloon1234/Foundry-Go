package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

func invalid(id string) {
	_ = datatable.Spec[reporting.Member, reporting.MemberRow, reporting.Authority]{ID: id}
}
