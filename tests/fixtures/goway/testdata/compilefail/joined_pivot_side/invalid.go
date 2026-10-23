package invalid

import (
	j "foundry.test/goway/models/joined"
)

var _ = j.Peers.WithPivot(j.TargetParent)
