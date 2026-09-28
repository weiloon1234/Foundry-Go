package invalid

import (
	"foundry.test/consumer/mutatorqueries"
)

var wrong string = (mutatorqueries.Member{}).FoundryReference().Key()
