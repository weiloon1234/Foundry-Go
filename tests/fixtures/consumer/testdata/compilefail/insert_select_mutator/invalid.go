package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.InsertArchiveFrom(linkqueries.QueryLinkMembers()).SelectTag(linkqueries.MemberFields().Name.Value())
}
