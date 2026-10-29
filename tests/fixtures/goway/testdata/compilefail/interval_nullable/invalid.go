package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() {
	_ = expr.NegateInterval(expr.Value(orm.NullableIntervalExpr(scalars.Records.MaybePeriod)))
}
