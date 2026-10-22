package models

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// LedgerEntry is a fixture for exact decimal hydration.
type LedgerEntry struct {
	ID      model.ID[LedgerEntry]
	Amount  decimal.Decimal
	Balance value.Nullable[decimal.Decimal]
}

// LedgerEntryTable binds LedgerEntry's persisted fields.
type LedgerEntryTable struct {
	orm.Table[LedgerEntry, model.ID[LedgerEntry]]
	ID      orm.Key[LedgerEntry, model.ID[LedgerEntry]]
	Amount  orm.Number[LedgerEntry, decimal.Decimal]
	Balance orm.NullableNumber[LedgerEntry, decimal.Decimal]
}

// LedgerEntries is the immutable ledger declaration.
var LedgerEntries = orm.Define("ledger_entries", func(b orm.Builder[LedgerEntry], l *LedgerEntry) LedgerEntryTable {
	table := orm.TableOf(b, &l.ID)
	return LedgerEntryTable{Table: table, ID: table.Key(), Amount: orm.NumberOf(b, &l.Amount), Balance: orm.NullableNumberOf(b, &l.Balance)}
})
