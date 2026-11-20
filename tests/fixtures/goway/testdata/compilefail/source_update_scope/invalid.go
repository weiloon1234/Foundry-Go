package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.UpdateFrom(models.WriteRecords.Query(), rows.From(models.Users.Query())).Select(rows.MapUpdate(models.WriteRecords.Name, expr.Value(models.WriteRecords.Name)))
