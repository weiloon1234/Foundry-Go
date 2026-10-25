package invalid

import (
	"foundry.test/consumer/vault"
)

func bad() { _ = vault.Entries.Token.Set("plaintext") }
