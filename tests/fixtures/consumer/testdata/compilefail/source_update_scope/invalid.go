package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).SelectName(linkqueries.MemberFields().Name.Value())
}
