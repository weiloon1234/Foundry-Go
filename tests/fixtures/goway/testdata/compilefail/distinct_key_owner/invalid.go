package distinct_key_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctOn(rows.FromModel(models.Users.Query()), expr.Group(expr.Value(orm.KeyExpr(models.Orders.ID))))
