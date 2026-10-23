package invalid

import (
	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

var _ = orm.Load[j.Parent]
var _ = expr.Count[j.Parent]
var _ decimal.Decimal
var invalid = func() { var _ orm.AggregateRelation[j.Parent, decimal.Decimal] = j.ParentAggregates.Total }
