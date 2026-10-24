package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DeleteUsing(models.Users.Query(), rows.From(models.Users.Query())).Values(models.Users.Age.Set(1))
