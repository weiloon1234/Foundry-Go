package invalid

import (
	"foundry.test/consumer/linkqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = linkqueries.UpdateMembershipFrom(linkqueries.QueryLinkMemberships(), linkqueries.QueryLinkMemberships()).SelectUpdatedAt(foundryquery.Value(linkqueries.MembershipFields().UpdatedAt))
}
