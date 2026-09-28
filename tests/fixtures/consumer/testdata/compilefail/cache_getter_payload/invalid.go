package invalid

import (
	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
)

func wrong(member mutatorqueries.Member) caching.Profile { return caching.Profile{Email: member.Email} }
