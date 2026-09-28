package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.DeleteMemberUsing(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).Values(linkqueries.MemberDraft{})
}
