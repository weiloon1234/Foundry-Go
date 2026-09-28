package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid(ctx context.Context, executor *database.Session) {
	q := models.QueryUsers()
	_, _ = query.SelectValue(q, models.UserFields().Email.Value()).
		LockRows(query.UpdateLock(q.Scope())).All(ctx, executor)
}
