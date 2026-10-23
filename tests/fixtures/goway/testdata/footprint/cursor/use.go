package cursor

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func Page(ctx context.Context, db database.Executor, r query.CursorRequest[models.User]) (query.CursorPage[models.User], error) {
	return orm.CursorPaginate(ctx, db, models.Users.Query().OrderBy(models.Users.Nickname.Asc()), r)
}
