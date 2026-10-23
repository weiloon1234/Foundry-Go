package invalid

import (
	s "foundry.test/goway/models/sensitive"
)

var _ = s.Accounts.Digest.Eq("plaintext")
