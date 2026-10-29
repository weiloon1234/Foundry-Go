package invalid

import (
	"foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.ConflictNull[sensitive.Vault, encrypted.Text](orm.NullableEncryptedExpr(sensitive.Vaults.Backup))
