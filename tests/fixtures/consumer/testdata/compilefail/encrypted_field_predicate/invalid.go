package invalid

import (
	"foundry.test/consumer/vault"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
)

func bad() { _ = vault.Entries.Token.Eq(encrypted.NewText("x")) }
