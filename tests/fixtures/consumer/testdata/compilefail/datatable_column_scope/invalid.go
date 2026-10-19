package invalid

import (
	"foundry.test/consumer/reporting"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var invalid = datatable.DefineColumn[reporting.Order](reporting.MemberRowValidationFields().Name, reporting.MemberNameLabel).SortBy(foundryquery.Value(reporting.MemberFields().Name))
