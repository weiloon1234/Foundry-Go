package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type other struct{}

func invalid() {
	_ = expr.AddIntervals(expr.Value(scalars.Records.Period), expr.At(rows.Scope(rows.As[other](scalars.Records.Query(), "s")), scalars.Records.Period))
}
