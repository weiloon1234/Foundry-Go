package invalid

import (
	h "foundry.test/goway/models/hooks"
)

var _ = h.Members.UpdatedAt.Increment(1)
