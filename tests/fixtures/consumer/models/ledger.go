package models

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type LedgerEntry struct {
	ID      model.ID[LedgerEntry]
	Amount  decimal.Decimal
	Balance value.Nullable[decimal.Decimal]
}
