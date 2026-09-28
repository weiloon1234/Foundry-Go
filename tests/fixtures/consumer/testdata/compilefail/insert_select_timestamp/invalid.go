package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.InsertArchiveFrom(linkqueries.QueryLinkMembers()).SelectUpdatedAt(linkqueries.MembershipFields().UpdatedAt.Value())
}
