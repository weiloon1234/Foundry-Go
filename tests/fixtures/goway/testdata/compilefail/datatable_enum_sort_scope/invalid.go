package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var column = datatable.Column(validation.DefineBinding("state", func(r *reporting.Member) *reporting.State { return &r.State }), "reports.state", expr.AsSelected(expr.CompareRow(expr.Value(reporting.Members.State))), datatable.SortBy(expr.AsSelected(expr.CompareRow(expr.Value(reporting.Members.State)))))
var invalid datatable.ColumnRegistration[reporting.Order, reporting.Member] = column
