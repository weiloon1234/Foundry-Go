package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() { _, _ = query.CursorFor(models.QueryUsers()).Create(nil, nil, models.UserDraft{}) }
