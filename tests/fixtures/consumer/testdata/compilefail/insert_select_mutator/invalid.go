package invalid

import (
	"foundry.test/consumer/linkqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = linkqueries.InsertArchiveFrom(linkqueries.QueryLinkMembers()).SelectTag(foundryquery.Value(linkqueries.MemberFields().Name))
}
