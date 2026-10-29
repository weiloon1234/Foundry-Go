package coalesce_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Coalesce(expr.Value(orm.NullableTextExpr(models.Users.Nickname)), expr.Value(orm.TextExpr(models.WriteRecords.Name)))
