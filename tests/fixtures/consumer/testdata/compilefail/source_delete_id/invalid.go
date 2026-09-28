package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.DeleteMemberUsing(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).MatchID(linkqueries.ArchiveFields().ID.Value())
}
