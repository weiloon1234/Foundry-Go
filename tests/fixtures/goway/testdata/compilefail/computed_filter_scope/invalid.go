package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

type Other struct{}

var _ datatable.RowEquality[Other, bool] = expr.CompareFilter(expr.JSONContains(expr.Value(jsonpaths.Documents.Settings), expr.Value(jsonpaths.Documents.Settings)))
