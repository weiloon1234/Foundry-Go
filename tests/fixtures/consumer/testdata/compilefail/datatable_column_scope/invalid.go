package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var invalid = datatable.DefineColumn[reporting.Order](reporting.MemberRowValidationFields().Name, reporting.MemberNameLabel).SortBy(reporting.MemberFields().Name.Value())
