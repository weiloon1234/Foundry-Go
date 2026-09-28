package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_, _ = query.CTE("users_copy", models.QueryUsers()).Create(context.Background(), nil, models.UserDraft{})
}
