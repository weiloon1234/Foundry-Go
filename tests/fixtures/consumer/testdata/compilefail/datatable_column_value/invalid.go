package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var sortBalance = datatable.SortBy(expr.AsSelected(expr.Row(expr.Value(reporting.MemberModels.Balance))))
var invalid = datatable.Column(reporting.MemberRowColumns().Name, reporting.MemberNameLabel, expr.AsSelected(expr.TextRow(reporting.MemberModels.Name)), sortBalance)
