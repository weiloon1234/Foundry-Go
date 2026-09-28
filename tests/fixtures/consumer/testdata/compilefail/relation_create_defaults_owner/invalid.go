package invalid

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _, _ = linkqueries.MembershipDraft{}.FoundryCreateMutation(query.Mutation[linkqueries.Friendship]{})
