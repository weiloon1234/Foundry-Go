package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.InsertArchiveFrom(linkqueries.QueryLinkMembers()).SelectName(linkqueries.MemberFields().Alias.Value())
}
