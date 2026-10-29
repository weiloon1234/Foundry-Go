package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func root() expr.JSONPath[jsonpaths.Document, jsonpaths.Preferences] {
	return expr.JSONRoot(expr.Value(orm.JSONExpr(jsonpaths.Documents.Settings)))
}
func invalid() {
	_ = expr.OrderNullableRow(expr.JSONScalar(expr.JSONOptional(expr.JSONProperty(root(), jsonpaths.Quota)), codec.Decimal())).Like("%")
}
