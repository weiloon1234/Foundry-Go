package invalid

import (
	"foundry.test/consumer/vault"
)

func bad() { _ = vault.EntryDraft{}.SetToken("plaintext") }
