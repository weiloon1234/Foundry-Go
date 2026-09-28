// Package models supplies handwritten declaration inputs for framework tests.
package models

import (
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

type CountryCode string

//foundry:model table=users
type User struct {
	ID                 model.ID[User]
	Email              string `foundry:"column=email_address"`
	Age                int
	Nickname           value.Nullable[string]
	Status             Status
	Level              Level
	Birthday           value.Nullable[temporal.Date]
	IntroducerID       value.Nullable[model.ID[User]]
	Introducer         relation.One[User]
	Referrals          relation.Many[User]
	Orders             relation.Many[Order]
	SingleOrder        relation.One[Order]
	Groups             relation.Through[Group, Membership]
	Friends            relation.Through[User, Friendship]
	Measurements       relation.Many[Measurement]
	OrderCount         relation.Value[int64]
	OrderTotal         relation.Value[value.Nullable[decimal.Decimal]]
	OrderAverage       relation.Value[value.Nullable[decimal.Decimal]]
	OrderMinimum       relation.Value[value.Nullable[int64]]
	OrderMaximum       relation.Value[value.Nullable[int64]]
	GroupCount         relation.Value[int64]
	GroupDistinct      relation.Value[int64]
	GroupPriority      relation.Value[value.Nullable[decimal.Decimal]]
	HasGroups          relation.Value[bool]
	MeasurementTotal   relation.Value[value.Nullable[decimal.Decimal]]
	MeasurementAverage relation.Value[value.Nullable[decimal.Decimal]]
	MeasurementScore   relation.Value[value.Nullable[float64]]
	MeasurementCount   relation.Value[int64]
	FirstLabel         relation.Value[value.Nullable[string]]
	Scratch            []string `foundry:"-"`
}

// CurrentEmailDraft deliberately refers to a generated return type and methods.
// Generation must work when all generated files are absent in a fresh checkout.
func (u User) CurrentEmailDraft() UserDraft { return UserDraft{}.SetEmail(u.Email) }

//foundry:model table=orders
type Order struct {
	ID         model.ID[Order]
	BuyerID    model.ID[User]
	TotalCents int64
	Buyer      relation.One[User]
}

//foundry:model table=countries primary=Code
type Country struct {
	Code      CountryCode
	Name      string
	Locations relation.Many[Location]
}
