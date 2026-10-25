package invalid

import (
	"foundry.test/consumer/vault"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func bad() { _ = orm.ConflictIncoming[vault.Entry, encrypted.Text](vault.Entries.Token) }
