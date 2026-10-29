package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.UpdateFrom(models.Users.Query(), rows.From(models.Users.Query())).Values(orm.NumberSet(models.Orders.TotalCents, 1))
