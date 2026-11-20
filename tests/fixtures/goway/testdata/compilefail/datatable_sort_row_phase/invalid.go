package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var name = expr.TextRow(reporting.Members.Name)
var invalid = datatable.SortBy[reporting.Member, string](name)
