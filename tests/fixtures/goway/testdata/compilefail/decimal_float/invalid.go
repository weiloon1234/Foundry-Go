package decimal_float

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models"
)

var _ = orm.NumberSet(m.LedgerEntries.Amount, 1.25)
