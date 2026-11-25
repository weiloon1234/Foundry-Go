// Package models supplies handwritten declaration inputs for framework tests.
package models

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin status-codec-declaration
//
//foundry:enum
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// doc-example:end status-codec-declaration

//foundry:enum
type Level uint8

const (
	LevelBasic    Level = 1
	LevelAdvanced Level = 2
)

type CountryCode string

//foundry:model table=users var=Users key=ID
type User struct {
	// ID selects the stored user key. Example: orm.Eq(Users.ID, id).
	ID model.ID[User] `foundry:"member=ID,key-role=primary"`
	// Email selects the stored address. Example: orm.Eq(Users.Email, email).
	Email string `foundry:"column=email_address"`
	Age   int
	// Nickname selects non-null nicknames. Example: orm.Eq(Users.Nickname, "member").
	Nickname value.Nullable[string]
	// Status selects the stored enum. Example: orm.Eq(Users.Status, models.StatusActive).
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

//foundry:model table=orders var=Orders key=ID identity=false
type Order struct {
	ID         model.ID[Order] `foundry:"member=ID,key-role=primary"`
	BuyerID    model.ID[User]
	TotalCents int64
	Buyer      relation.One[User]
}

//foundry:model table=countries var=Countries key=Code identity=false
type Country struct {
	// Code selects the stored natural key. Example: orm.Eq(Countries.Code, models.CountryCode("MY")).
	Code CountryCode `foundry:"member=Code,key-role=primary"`
	// Name selects a stored name. Example: orm.Eq(Countries.Name, "Malaysia").
	Name      string
	Locations relation.Many[Location]
}
