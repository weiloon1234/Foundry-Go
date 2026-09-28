package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

func invalid(label string) {
	_ = datatable.DefineColumn[reporting.Member](reporting.MemberRowValidationFields().Name, label)
}
