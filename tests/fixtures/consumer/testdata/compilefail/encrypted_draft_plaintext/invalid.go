package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/vault"
)

func bad() { _ = orm.Set(vault.Entries.Token, "plaintext") }
