package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

type Other struct{}

var _ datatable.RowNullable[Other, bool] = expr.NullableCompareFilter(expr.JSONContainsNullable(expr.Value(orm.NullableJSONExpr(jsonpaths.Documents.Backup)), expr.Value(orm.NullableJSONExpr(jsonpaths.Documents.Backup))))
