package invalid

import (
	"foundry.test/consumer/linkqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).SelectID(foundryquery.Value(linkqueries.ArchiveFields().MemberID))
}
