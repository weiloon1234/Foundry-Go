package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var _ datatable.RowNullable[jsonqueries.Policy, bool] = expr.NullableCompareFilter(expr.JSONContainsNullable(expr.Value(orm.NullableJSONExpr(jsonqueries.Documents.Backup)), expr.Value(orm.NullableJSONExpr(jsonqueries.Documents.Backup))))
