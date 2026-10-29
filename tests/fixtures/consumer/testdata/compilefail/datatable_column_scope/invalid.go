package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var invalid = datatable.DefineColumn[reporting.Order](reporting.MemberRowValidationFields().Name, reporting.MemberNameLabel).SortBy(expr.Row(expr.Value(orm.TextExpr(reporting.MemberModels.Name))).Value())
