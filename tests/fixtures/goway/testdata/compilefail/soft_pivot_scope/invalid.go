package invalid

import (
	j "foundry.test/goway/models/joined"
)

var _ = j.Peers.OnlyTrashedPivot().WherePivot(j.Parents.Name.Eq("wrong model"))
