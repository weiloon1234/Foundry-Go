package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type other struct{}

func invalid() {
	_ = expr.Concat(expr.Value(orm.TextExpr(scalars.Records.Name)), expr.At(rows.Scope(rows.As[other](scalars.Records.Query(), "s")), orm.TextExpr(scalars.Records.Name)))
}
