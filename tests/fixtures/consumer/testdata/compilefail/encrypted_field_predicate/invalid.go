package invalid

import (
	"foundry.test/consumer/vault"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func bad() { _ = orm.ColumnEq(vault.Entries.Token, encrypted.NewText("x")) }
