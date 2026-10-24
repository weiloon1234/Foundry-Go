package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func invalid() {
	_ = intervalqueries.Samples.Query().Where(expr.SumInterval(intervalqueries.Samples.Period).Gt(temporal.Days(1)))
}
