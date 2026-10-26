package cursor

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Page(ctx context.Context, db database.Executor, r orm.CursorRequest[models.User]) (orm.CursorPage[models.User], error) {
	return orm.CursorPaginate(ctx, db, models.Users.Query().OrderBy(models.Users.Nickname.Asc()), r)
}
