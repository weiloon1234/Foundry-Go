package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.CompareFilter[jsonqueries.Document, bool](expr.AsSelected(expr.JSONContains(expr.Value(orm.JSONExpr(jsonqueries.Documents.Settings)), expr.Value(orm.JSONExpr(jsonqueries.Documents.Settings)))))
