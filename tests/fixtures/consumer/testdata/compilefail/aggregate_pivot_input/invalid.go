package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Related(orm.Pivot(models.UserLinks.Groups), expr.CountDistinctField(orm.KeyExpr(models.Groups.Code)))
