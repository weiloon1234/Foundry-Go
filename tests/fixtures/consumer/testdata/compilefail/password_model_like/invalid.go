package invalid

import (
	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func bad() { _ = orm.TextLike(passwords.AccountRecords.Digest, "%hash%") }
