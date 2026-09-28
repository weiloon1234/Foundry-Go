package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	var rows []linkqueries.Member
	rows, _ = linkqueries.InsertArchiveFrom(linkqueries.QueryLinkMembers()).Returning(context.Background(), db, 1)
	_ = rows
}
