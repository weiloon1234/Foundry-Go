package models

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
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

// UserTable binds User's persisted fields.
type UserTable struct {
	orm.Table[User, model.ID[User]]
	ID           orm.Key[User, model.ID[User]]
	Email        orm.Text[User, string]
	Age          orm.Number[User, int]
	Nickname     orm.NullableText[User, string]
	Status       orm.Enum[User, Status]
	Level        orm.Enum[User, Level]
	Birthday     orm.NullableTime[User, temporal.Date]
	IntroducerID orm.NullableColumn[User, model.ID[User]]
	Introducer   orm.One[User, User]
	Orders       orm.Many[User, Order]
}

// Users is the immutable user declaration.
var Users = orm.Define("users", func(b orm.Builder[User], u *User) UserTable {
	table := orm.TableOf(b, &u.ID)
	return UserTable{Table: table, ID: table.Key(), Email: orm.TextOf(b, &u.Email), Age: orm.NumberOf(b, &u.Age), Nickname: orm.NullableTextOf(b, &u.Nickname), Status: orm.EnumOf(b, &u.Status), Level: orm.EnumOf(b, &u.Level), Birthday: orm.NullableTimeOf(b, &u.Birthday), IntroducerID: orm.NullableIDOf(b, &u.IntroducerID), Introducer: orm.OneOf(b, &u.Introducer), Orders: orm.ManyOf(b, &u.Orders)}
})
