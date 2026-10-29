package case_result_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.When(expr.OrderRow(expr.Value(orm.NumberExpr(models.Users.Age))).Gt(0), expr.Value(orm.TextExpr(models.WriteRecords.Name)))
