package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/vault"
)

func bad() { _ = orm.EncryptedSet(vault.Entries.Token, "plaintext") }
