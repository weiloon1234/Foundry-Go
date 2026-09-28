package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	var row linkqueries.Member
	row, _ = linkqueries.QueryLinkMemberships().FirstOrCreate(context.Background(), db, linkqueries.MembershipDraft{})
	_ = row
}
