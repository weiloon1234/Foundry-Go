package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

func wrong() {
	item, _ := models.QueryUsers().Upsert(nil, nil, models.UserDraft{}, query.OnConflict[models.User]().DoNothing())
	var _ value.Optional[models.Order] = item
}
