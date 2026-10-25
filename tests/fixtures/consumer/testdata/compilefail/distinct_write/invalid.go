package invalid

import (
	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DistinctRecord(rows.SelectRecord(rows.From(models.Users.Query()), rows.ModelScope(models.Users.Query()))).Update
