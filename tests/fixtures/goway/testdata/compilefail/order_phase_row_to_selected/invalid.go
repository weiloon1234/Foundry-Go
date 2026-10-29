package order_phase_row_to_selected

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.SelectedOrder[models.User](expr.Value(orm.NumberExpr(models.Users.Age)).Asc())
