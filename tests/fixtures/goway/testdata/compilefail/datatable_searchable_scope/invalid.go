package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var name = expr.TextRow(orm.TextExpr(reporting.Members.Name))
var invalid = datatable.DefineColumn[reporting.Order, reporting.Member, string](
	validation.DefineField("name", func(r reporting.Member) string { return r.Name }),
	"reports.name",
	datatable.Searchable(name),
)
