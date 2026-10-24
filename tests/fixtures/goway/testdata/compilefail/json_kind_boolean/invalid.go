package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() { _ = expr.JSONType(expr.Value(jsonpaths.Documents.Settings)).Eq(true) }
