package invalid

import (
	j "foundry.test/goway/models/joined"
)

var _ = j.Peers.OrderByPivot(j.Targets.Name.Asc())
