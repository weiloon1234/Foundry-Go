package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

func invalid(label string) {
	_ = datatable.Column(reporting.MemberRowColumns().Name, label, expr.AsSelected(expr.TextRow(reporting.MemberModels.Name)))
}
