package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var integerCell = datatable.ScalarCell(foundryhttp.IntegerQuery[int64]())
var invalid = datatable.Column(reporting.MemberRowColumns().Name, reporting.MemberNameLabel, expr.AsSelected(expr.TextRow(reporting.MemberModels.Name)), datatable.ExportAs(expr.AsSelected(expr.TextRow(reporting.MemberModels.Name)), integerCell))
