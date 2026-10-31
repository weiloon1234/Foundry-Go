package distinct_predicate_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Distinct(rows.FromModel(models.Users.Query())).Where(expr.Eq(expr.Value(orm.NumberExpr(models.Orders.TotalCents)), 3))
