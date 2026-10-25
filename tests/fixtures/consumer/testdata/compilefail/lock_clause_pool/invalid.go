package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func invalid(ctx context.Context, executor *database.DB) {
	_, _ = rows.LockRows(rows.ReadScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.Email)))), rows.UpdateLock(rows.Target(rows.ModelScope(models.Users.Query())))).All(ctx, executor)
}
