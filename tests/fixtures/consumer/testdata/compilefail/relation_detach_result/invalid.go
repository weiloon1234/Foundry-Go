package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	rows, _ := linkqueries.MemberRelations().Groups.Detach(context.Background(), db, linkqueries.Member{}, linkqueries.Group{})
	var wrong []linkqueries.Friendship = rows
	_ = wrong
}
