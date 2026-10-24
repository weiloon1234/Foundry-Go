package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func invalid() { _ = expr.SumInterval(intervalqueries.Samples.Period).Gt(decimal.Decimal{}) }
