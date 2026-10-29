package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	_ = expr.JSONContains(expr.Value(orm.NullableJSONExpr(jsonpaths.Documents.Backup)), expr.Value(orm.JSONExpr(jsonpaths.Documents.Settings)))
}
