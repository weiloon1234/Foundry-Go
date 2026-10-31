package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var _ = datatable.Where(foundryhttp.StringQuery[string](), expr.CompareFilter(expr.JSONContains(expr.Value(orm.JSONExpr(jsonqueries.Documents.Settings)), expr.Value(orm.JSONExpr(jsonqueries.Documents.Settings)))))
