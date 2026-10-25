package invalid

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

func invalid() {
	f := temporalqueries.Samples
	_ = expr.AddDateInterval(expr.Value(f.Date), temporal.Days(1)).Eq(temporal.Date{})
}
