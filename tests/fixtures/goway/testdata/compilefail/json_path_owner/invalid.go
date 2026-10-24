package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func root() expr.JSONPath[jsonpaths.Document, jsonpaths.Preferences] {
	return expr.JSONRoot(expr.Value(jsonpaths.Documents.Settings))
}

type Other struct{}

func invalid() {
	var q orm.Query[Other]
	_ = expr.Where(q, expr.JSONTextScalar(expr.JSONOptional(expr.JSONProperty(root(), jsonpaths.Theme)), codec.String[string]()).Eq("dark"))
}
