package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var sortMember = datatable.SortBy(expr.AsSelected(expr.Row(expr.Value(reporting.MemberModels.Name))))
var invalid = datatable.Column(reporting.MemberRowColumns().Name, reporting.MemberNameLabel, expr.AsSelected(expr.TextRow(reporting.OrderModels.Label)), sortMember)
