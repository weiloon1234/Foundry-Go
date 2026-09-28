// Package windowqueries declares complete typed window-function results.
package windowqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:projection
type RankingRow struct {
	ID         model.ID[models.Order]
	BuyerID    model.ID[models.User]
	Number     int64
	Rank       int64
	Dense      int64
	Percent    float64
	Cumulative float64
	Bucket     int32
}

//foundry:projection
type NavigationRow struct {
	Amount   int64
	Previous value.Nullable[int64]
	Next     value.Nullable[int64]
	Fallback int64
	First    value.Nullable[int64]
	Last     value.Nullable[int64]
	Nth      value.Nullable[int64]
}

//foundry:projection
type WindowTotal struct {
	Amount  int64
	Running value.Nullable[decimal.Decimal]
	Count   int64
	Any     bool
}
