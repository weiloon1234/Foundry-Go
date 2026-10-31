package invalid

import (
	"foundry.test/goway/reporting"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var invalid = datatable.DefineColumn[reporting.Order, reporting.Member, string](
	validation.DefineField("name", func(r reporting.Member) string { return r.Name }),
	"reports.name",
	datatable.FilterBy(datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(expr.TextRow(orm.TextExpr(reporting.Members.Name))))),
)
