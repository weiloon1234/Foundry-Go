package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var exportBalance = datatable.ExportAs(expr.AsSelected(expr.Row(expr.Value(reporting.MemberModels.Balance))), datatable.ScalarCell(foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()))
var invalid = datatable.Column(
	reporting.MemberRowColumns().Name,
	reporting.MemberNameLabel,
	expr.AsSelected(expr.TextRow(reporting.MemberModels.Name)),
	exportBalance,
)
