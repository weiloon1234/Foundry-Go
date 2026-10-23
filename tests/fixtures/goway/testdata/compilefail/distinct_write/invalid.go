package distinct_write

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Distinct(rows.FromModel(models.Users.Query())).Update
