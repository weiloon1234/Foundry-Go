package invalid

import (
	"context"
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(db *database.DB) {
	_, _ = orm.ForceDelete(context.Background(), db, softqueries.Members.Query().WithTrashed(), softqueries.Members.Soft, softqueries.Members.Key(), model.ID[softqueries.Membership]{})
}
