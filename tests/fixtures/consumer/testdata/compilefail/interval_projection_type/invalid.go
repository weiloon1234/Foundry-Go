package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func invalid() {
	_ = rows.MapSelected(intervalqueries.Summaries.Count, expr.SumInterval(intervalqueries.Samples.Period).Value())
}
