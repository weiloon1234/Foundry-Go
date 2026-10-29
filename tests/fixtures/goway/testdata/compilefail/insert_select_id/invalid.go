package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

var destination orm.Primary[models.User, model.ID[models.User]]
var _ = rows.MapInsert(orm.KeyExpr(destination), expr.Value(orm.KeyExpr(models.Orders.ID)))
