package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type other struct{}

func invalid() {
	_ = expr.AddIntervals(expr.Value(orm.IntervalExpr(scalars.Records.Period)), expr.At(rows.Scope(rows.As[other](scalars.Records.Query(), "s")), orm.IntervalExpr(scalars.Records.Period)))
}
