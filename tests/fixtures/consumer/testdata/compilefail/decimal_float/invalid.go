package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/models"

var invalid = orm.NumberSet(models.LedgerEntries.Amount, 1.25)
