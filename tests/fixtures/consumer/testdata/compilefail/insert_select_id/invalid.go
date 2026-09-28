package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.InsertArchiveFrom(linkqueries.QueryLinkMembers()).SelectID(linkqueries.MemberFields().ID.Value())
}
