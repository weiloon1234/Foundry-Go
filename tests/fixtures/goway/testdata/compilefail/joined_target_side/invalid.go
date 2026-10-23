package invalid

import (
	j "foundry.test/goway/models/joined"
)

var _ = j.Peers.Where(j.Pivots.Role.Eq("admin"))
