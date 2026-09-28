package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var invalid datatable.ColumnRegistration[reporting.Member, reporting.OrderRow] = datatable.DefineColumn[reporting.Member](reporting.MemberRowValidationFields().Name, reporting.MemberNameLabel).Registration()
