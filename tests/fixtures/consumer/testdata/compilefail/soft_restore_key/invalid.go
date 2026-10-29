package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(db *database.DB) {
	_, _ = orm.Restore(context.Background(), db, softqueries.Members.Table, model.ID[softqueries.Membership]{})
}
