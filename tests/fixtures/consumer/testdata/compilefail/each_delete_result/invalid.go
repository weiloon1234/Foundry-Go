package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	rows, _ := linkqueries.QueryLinkMemberships().DeleteEach(context.Background(), db, 10)
	var wrong []linkqueries.Member = rows
	_ = wrong
}
