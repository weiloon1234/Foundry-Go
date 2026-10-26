package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var invalid = datatable.DefineColumn[reporting.Order](validation.DefineField("state", func(r reporting.Member) reporting.State { return r.State }), "reports.state").SortBy(expr.CompareRow(expr.Value(reporting.Members.State)).Value())
