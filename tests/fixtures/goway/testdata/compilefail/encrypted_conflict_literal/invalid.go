package invalid

import (
	"foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.ConflictSet[sensitive.Vault, encrypted.Text, encrypted.Text](sensitive.Vaults.Token, encrypted.Text{})
