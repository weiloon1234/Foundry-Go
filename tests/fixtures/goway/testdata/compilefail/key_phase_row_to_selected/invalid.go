package key_phase_row_to_selected

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.ProjectionKey[models.User](expr.Group(expr.Value(orm.NumberExpr(models.Users.Age))))
