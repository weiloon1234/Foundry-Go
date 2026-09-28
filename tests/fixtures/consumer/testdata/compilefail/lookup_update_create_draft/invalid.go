package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	_, _ = linkqueries.QueryLinkMemberships().UpdateOrCreate(context.Background(), db, linkqueries.MemberDraft{}, func(context.Context, *database.Tx, linkqueries.Membership) (linkqueries.MembershipDraft, error) {
		return linkqueries.MembershipDraft{}, nil
	})
}
