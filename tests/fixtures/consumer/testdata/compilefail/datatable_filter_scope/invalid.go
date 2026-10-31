package invalid

import (
	"foundry.test/consumer/reporting"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.DefineColumn[reporting.Order, reporting.MemberRow, string](
	reporting.MemberRowValidationFields().Name,
	reporting.MemberNameLabel,
	datatable.FilterBy(datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(expr.TextRow(orm.TextExpr(reporting.MemberModels.Name))))),
)
