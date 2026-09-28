package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	_, _ = linkqueries.QueryLinkMemberships().UpdateEach(context.Background(), db, 10, func(context.Context, *database.Tx, linkqueries.Member) (linkqueries.MembershipDraft, error) {
		return linkqueries.MembershipDraft{}, nil
	})
}
