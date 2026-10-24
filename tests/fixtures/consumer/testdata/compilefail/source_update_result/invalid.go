package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func invalid(ctx context.Context, db database.Transactor) {
	var result []linkqueries.Archive
	result, _ = rows.UpdateFrom(linkqueries.Members.Query(), rows.From(linkqueries.Archives.Query())).Returning(ctx, db, 1)
	_ = result
}
