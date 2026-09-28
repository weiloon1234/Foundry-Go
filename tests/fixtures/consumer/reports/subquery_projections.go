package reports

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:projection
type UserOrderStats struct {
	ID     model.ID[models.User]
	Email  string
	Total  value.Nullable[decimal.Decimal]
	Orders value.Nullable[int64]
}

//foundry:projection
type ScalarReport struct {
	ID       model.ID[models.User]
	BuyerID  value.Nullable[model.ID[models.User]]
	Nickname value.Nullable[string]
	Orders   value.Nullable[int64]
}
