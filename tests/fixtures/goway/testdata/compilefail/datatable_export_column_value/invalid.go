package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var balance = expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(reporting.Members.Balance))))
var invalid = datatable.DefineColumn[reporting.Member, reporting.Member, string](
	validation.DefineField("name", func(r reporting.Member) string { return r.Name }),
	"reports.name",
	datatable.ExportAs(balance, datatable.ScalarCell(foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]())),
)
