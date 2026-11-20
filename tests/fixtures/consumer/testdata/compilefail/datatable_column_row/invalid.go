package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var invalid datatable.ColumnRegistration[reporting.Member, reporting.OrderRow] = datatable.Column(reporting.MemberRowColumns().Name, reporting.MemberNameLabel, expr.AsSelected(expr.TextRow(reporting.MemberModels.Name)))
