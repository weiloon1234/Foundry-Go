package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	_, _ = linkqueries.MemberRelations().Groups.Attach(context.Background(), db, linkqueries.Member{}, linkqueries.Group{}, linkqueries.FriendshipDraft{})
}
