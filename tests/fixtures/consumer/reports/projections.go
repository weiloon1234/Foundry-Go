// Package reports declares independent read results, separate from persisted models.
package reports

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin user-summary-record
type UserSummary struct {
	ID model.ID[models.User]
	// Email is the buyer contact address selected for this report.
	Email    string `foundry:"column=contact_email"`
	Nickname value.Nullable[string]
	Status   models.Status
}

// doc-example:end user-summary-record

type BuyerTotals struct {
	BuyerID model.ID[models.User]
	Total   value.Nullable[decimal.Decimal]
	Orders  int64
	Average value.Nullable[decimal.Decimal]
}

type OrderSummary struct {
	Total value.Nullable[decimal.Decimal]
	Count int64
}
