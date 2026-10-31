package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var balance = expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(reporting.MemberModels.Balance))))
var invalid = datatable.DefineColumn[reporting.Member, reporting.MemberRow, string](
	reporting.MemberRowValidationFields().Name,
	reporting.MemberNameLabel,
	datatable.ExportAs(balance, datatable.ScalarCell(foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]())),
)
