package oldmodels

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:enum
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

//foundry:enum
type Level uint8

const (
	LevelBasic    Level = 1
	LevelAdvanced Level = 2
)

//foundry:model table=users
type User struct {
	ID           model.ID[User]
	Email        string `foundry:"column=email_address"`
	Age          int
	Nickname     value.Nullable[string]
	Status       Status
	Level        Level
	Birthday     value.Nullable[temporal.Date]
	IntroducerID value.Nullable[model.ID[User]]
	Introducer   relation.One[User]
	Orders       relation.Many[Order]
}

//foundry:model table=orders
type Order struct {
	ID         model.ID[Order]
	BuyerID    model.ID[User]
	TotalCents int64
	Buyer      relation.One[User]
}

//foundry:model table=ledger_entries
type LedgerEntry struct {
	ID      model.ID[LedgerEntry]
	Amount  decimal.Decimal
	Balance value.Nullable[decimal.Decimal]
}
type RecordKey int64

//foundry:model table=write_records primary=Code
type WriteRecord struct {
	Code    RecordKey `foundry:"default=database"`
	Name    string
	Enabled bool `foundry:"default=database"`
	Note    value.Nullable[string]
	Amount  decimal.Decimal `foundry:"default=database"`
}

func defineUserRelations() UserRelationSet {
	return UserRelationSet{
		Introducer: query.BelongsTo(UserFields().IntroducerID, UserFields().ID),
		Orders:     query.HasMany(UserFields().ID, OrderFields().BuyerID),
	}
}
func defineOrderRelations() OrderRelationSet {
	return OrderRelationSet{Buyer: query.BelongsTo(OrderFields().BuyerID, UserFields().ID)}
}
