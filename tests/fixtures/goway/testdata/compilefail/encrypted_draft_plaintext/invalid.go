package invalid

import (
	s "foundry.test/goway/models/sensitive"
)

var _ = s.Vaults.Token.Set("plaintext")
