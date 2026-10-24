package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.UpdateFrom(models.Users.Query(), rows.From(models.Users.Query())).Values(models.Orders.TotalCents.Set(1))
