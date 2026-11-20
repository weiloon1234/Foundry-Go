package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var column = datatable.Column(validation.DefineBinding("name", func(r *reporting.Member) *string { return &r.Name }), "reports.name", expr.AsSelected(expr.TextRow(reporting.Members.Name)))
var invalid datatable.ColumnRegistration[reporting.Member, reporting.OrderRow] = column
