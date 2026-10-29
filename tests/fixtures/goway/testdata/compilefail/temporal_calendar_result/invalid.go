package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type other struct{}

func invalid() {
	_ = expr.AddDateInterval(expr.Value(orm.TimeExpr(scalars.Records.Date)), temporal.Days(1)).Eq(temporal.Date{})
}
