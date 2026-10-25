package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctOnScalarValues(rows.SelectScalar(rows.From(models.Users.Query()), expr.Count[models.User]().Value()), expr.Count[models.Order]().Value().Key())
