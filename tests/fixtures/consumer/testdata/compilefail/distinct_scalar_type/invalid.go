package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.InQuery(expr.Value(models.Users.ID), rows.DistinctScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(models.Users.Email)).Value())))
