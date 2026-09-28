package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	_, _ = linkqueries.QueryLinkMemberships().CreateEach(context.Background(), db, []linkqueries.MemberDraft{})
}
