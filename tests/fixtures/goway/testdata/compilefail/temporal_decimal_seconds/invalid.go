package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() { _ = expr.Eq(expr.Second(expr.Value(orm.TimeExpr(scalars.Records.Clock))), float64(1)) }
