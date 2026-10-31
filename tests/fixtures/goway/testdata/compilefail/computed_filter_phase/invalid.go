package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.CompareFilter[jsonpaths.Document, bool](expr.AsSelected(expr.JSONContains(expr.Value(orm.JSONExpr(jsonpaths.Documents.Settings)), expr.Value(orm.JSONExpr(jsonpaths.Documents.Settings)))))
