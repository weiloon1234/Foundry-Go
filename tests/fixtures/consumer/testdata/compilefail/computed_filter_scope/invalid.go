package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var _ datatable.RowEquality[jsonqueries.Policy, bool] = expr.CompareFilter(expr.JSONContains(expr.Value(orm.JSONExpr(jsonqueries.Documents.Settings)), expr.Value(orm.JSONExpr(jsonqueries.Documents.Settings))))
