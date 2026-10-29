package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/value"
)

func root() expr.JSONPath[jsonpaths.Document, jsonpaths.Preferences] {
	return expr.JSONRoot(expr.Value(orm.JSONExpr(jsonpaths.Documents.Settings)))
}
func invalid() {
	var _ expr.RowExpression[jsonpaths.Document, value.Nullable[value.JSON[int]]] = expr.JSONOptional(expr.JSONProperty(root(), jsonpaths.Theme)).JSON()
}
