package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

func invalid() {
	f := temporalqueries.Samples
	_ = expr.Eq(expr.AddDateInterval(expr.Value(orm.TimeExpr(f.Date)), temporal.Days(1)), temporal.Date{})
}
