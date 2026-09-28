package invalid

import (
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ value.Optional[string] = (mutatorqueries.MemberChanges{}).Fields().Nickname.After()
