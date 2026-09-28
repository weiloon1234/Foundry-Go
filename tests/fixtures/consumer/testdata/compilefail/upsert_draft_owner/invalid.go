package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _, _ = models.QueryUsers().Upsert(nil, nil, models.OrderDraft{}, query.OnConflict[models.User]().DoNothing())
