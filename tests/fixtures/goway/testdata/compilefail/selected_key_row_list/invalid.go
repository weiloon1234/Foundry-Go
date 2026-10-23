package selected_key_row_list

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctOn(rows.FromModel(models.Users.Query()), expr.Select(expr.Value(models.Users.Age)).Key())
