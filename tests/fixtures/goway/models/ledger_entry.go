package models

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// LedgerEntry is a fixture for exact decimal hydration.
//
//foundry:model table=ledger_entries var=LedgerEntries key=ID identity=false
type LedgerEntry struct {
	ID      model.ID[LedgerEntry]           `foundry:"member=ID"`
	Amount  decimal.Decimal                 `foundry:"role=number"`
	Balance value.Nullable[decimal.Decimal] `foundry:"role=number"`
}
