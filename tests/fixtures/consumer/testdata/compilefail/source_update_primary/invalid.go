package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).SelectID(linkqueries.ArchiveFields().MemberID.Value())
}
