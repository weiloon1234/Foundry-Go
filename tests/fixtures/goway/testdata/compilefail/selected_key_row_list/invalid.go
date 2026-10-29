package selected_key_row_list

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctOn(rows.FromModel(models.Users.Query()), expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))).Value().Key())
