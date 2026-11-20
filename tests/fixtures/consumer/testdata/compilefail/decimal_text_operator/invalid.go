package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid = orm.Contains(models.LedgerEntries.Amount, "1")
