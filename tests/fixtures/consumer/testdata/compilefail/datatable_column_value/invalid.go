package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var invalid = datatable.DefineColumn[reporting.Member, reporting.MemberRow, string](reporting.MemberRowValidationFields().Name, reporting.MemberNameLabel, datatable.SortBy(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(reporting.MemberModels.Balance))))))
