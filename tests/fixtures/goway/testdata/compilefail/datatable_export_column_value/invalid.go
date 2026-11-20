package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var balance = expr.AsSelected(expr.Row(expr.Value(reporting.Members.Balance)))
var option = datatable.ExportAs(balance, datatable.ScalarCell(foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()))
var invalid = datatable.Column(
	validation.DefineBinding("name", func(r *reporting.Member) *string { return &r.Name }),
	"reports.name",
	expr.AsSelected(expr.Row(expr.Value(reporting.Members.Name))),
	option,
)
