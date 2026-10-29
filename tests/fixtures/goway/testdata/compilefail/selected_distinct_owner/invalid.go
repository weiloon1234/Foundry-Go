package selected_distinct_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctOnValues(rows.FromModel(models.Users.Query()), expr.Row(expr.Value(orm.NumberExpr(models.Orders.TotalCents))).Value().Key())
