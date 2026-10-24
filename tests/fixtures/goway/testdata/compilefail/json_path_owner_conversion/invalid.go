package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func root() expr.JSONPath[jsonpaths.Document, jsonpaths.Preferences] {
	return expr.JSONRoot(expr.Value(jsonpaths.Documents.Settings))
}

type Other struct{}

func invalid() { _ = expr.JSONPath[Other, jsonpaths.Preferences](root()) }
