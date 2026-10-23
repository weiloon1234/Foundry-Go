package invalid

import (
	j "foundry.test/goway/models/joined"
)

var _ = j.Peers.WherePivot(j.Targets.Name.Eq("member"))
