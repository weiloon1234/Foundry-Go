package invalid

import (
	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ value.Optional[value.Nullable[inputqueries.StoredLabel]] = inputqueries.MemberDraft{}.Note()
