package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type wrapped struct {
	orm.ExpressionColumn[models.User, string, string]
}

var _ = rows.MapUpdate(wrapped{orm.TextExpr(models.Users.Email)}, expr.Value(orm.TextExpr(models.Users.Email)))
