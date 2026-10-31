package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var invalid = datatable.DefineColumn[reporting.Order, reporting.Member, reporting.State](validation.DefineField("state", func(r reporting.Member) reporting.State { return r.State }), "reports.state", datatable.SortBy(expr.AsSelected(expr.CompareRow(expr.Value(orm.EnumExpr(reporting.Members.State))))))
