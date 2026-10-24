package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() { _ = expr.NegateInterval(expr.SumInterval(intervalqueries.Samples.Period).Value()) }
