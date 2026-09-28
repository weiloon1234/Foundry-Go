package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.UpdateMembershipFrom(linkqueries.QueryLinkMemberships(), linkqueries.QueryLinkMemberships()).SelectUpdatedAt(linkqueries.MembershipFields().UpdatedAt.Value())
}
