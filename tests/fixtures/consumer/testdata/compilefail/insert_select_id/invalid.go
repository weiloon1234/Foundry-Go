package invalid

import (
	"foundry.test/consumer/linkqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = linkqueries.InsertArchiveFrom(linkqueries.QueryLinkMembers()).SelectID(foundryquery.Value(linkqueries.MemberFields().ID))
}
