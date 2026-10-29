package invalid

import (
	"foundry.test/consumer/vault"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func bad() { _ = orm.TextAsc(vault.Entries.Token) }
