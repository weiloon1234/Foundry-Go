package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = rows.RowLock[models.User](rows.RowLock[models.Country]{})
