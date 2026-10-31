package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() { _ = expr.TextLike(expr.Lower(expr.Value(orm.TextExpr(scalars.Records.Name))), 123) }
