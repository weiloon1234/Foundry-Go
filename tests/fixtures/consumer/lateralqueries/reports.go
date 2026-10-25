package lateralqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type UserOrder struct {
	UserID  model.ID[models.User]
	OrderID value.Nullable[model.ID[models.Order]]
	Total   value.Nullable[int64]
}

type RankedOrder struct {
	ID    model.ID[models.Order]
	Total int64
	Rank  int64
}
