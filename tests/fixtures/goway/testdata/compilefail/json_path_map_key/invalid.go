package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func root() expr.JSONPath[jsonpaths.Document, jsonpaths.Preferences] {
	return expr.JSONRoot(expr.Value(jsonpaths.Documents.Settings))
}
func invalid() {
	p := expr.JSONOptional(expr.JSONProperty(root(), jsonpaths.UserProfile))
	_ = expr.JSONMapKey(expr.JSONProperty(p, jsonpaths.Attributes), "2")
}
