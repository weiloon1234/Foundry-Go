// Package reports declares independent read results, separate from persisted models.
package reports

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:projection
type UserSummary struct {
	ID       model.ID[models.User]
	Email    string `foundry:"column=contact_email"`
	Nickname value.Nullable[string]
	Status   models.Status
}

//foundry:projection
type BuyerTotals struct {
	BuyerID model.ID[models.User]
	Total   value.Nullable[decimal.Decimal]
	Orders  int64
	Average value.Nullable[decimal.Decimal]
}

//foundry:projection
type OrderSummary struct {
	Total value.Nullable[decimal.Decimal]
	Count int64
}
