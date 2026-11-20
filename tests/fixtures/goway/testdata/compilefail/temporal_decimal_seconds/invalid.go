package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() { _ = expr.Eq(expr.Second(expr.Value(scalars.Records.Clock)), float64(1)) }
