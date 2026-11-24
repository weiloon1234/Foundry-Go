package models

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Status is the fixture's generated string enum.
//
//foundry:enum
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// Level is the fixture's generated numeric enum.
//
//foundry:enum
type Level uint8

const (
	LevelBasic    Level = 1
	LevelAdvanced Level = 2
)

// User keeps every persisted field of the original fixture, in source order.
//
//foundry:model table=users var=Users key=ID identity=false
type User struct {
	ID           model.ID[User] `foundry:"member=ID"`
	Email        string         `foundry:"column=email_address"`
	Age          int
	Nickname     value.Nullable[string]
	Status       Status
	Level        Level
	Birthday     value.Nullable[temporal.Date]
	IntroducerID value.Nullable[model.ID[User]]
	Introducer   relation.One[User]
	Orders       relation.Many[Order]
}
