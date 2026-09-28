package invalid

import (
	"context"
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(db *database.DB) {
	_, _ = softqueries.QuerySoftMembers().WithTrashed().ForceDelete(context.Background(), db, model.ID[softqueries.Membership]{})
}
