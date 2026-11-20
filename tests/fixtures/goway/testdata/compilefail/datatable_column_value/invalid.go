package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var option = datatable.SortBy(expr.AsSelected(expr.Row(expr.Value(reporting.Members.Balance))))
var invalid = datatable.Column(validation.DefineBinding("name", func(r *reporting.Member) *string { return &r.Name }), "reports.name", expr.AsSelected(expr.Row(expr.Value(reporting.Members.Name))), option)
