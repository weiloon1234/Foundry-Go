package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid(ctx context.Context, executor *database.DB) {
	q := models.QueryUsers()
	_, _ = query.SelectValue(q, query.Value(models.UserFields().Email)).
		LockRows(query.UpdateLock(q.Scope())).All(ctx, executor)
}
