package invalid

import (
	"foundry.test/consumer/mutatorqueries"
)

var wrong string = mutatorqueries.MemberIdentity.ReferenceOf(mutatorqueries.Member{}).Key()
