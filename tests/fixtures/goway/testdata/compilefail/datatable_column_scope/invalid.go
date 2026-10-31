package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var invalid = datatable.DefineColumn[reporting.Order, reporting.Member, string](validation.DefineField("name", func(r reporting.Member) string { return r.Name }), "reports.name", datatable.SortBy(expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(reporting.Members.Name))))))
