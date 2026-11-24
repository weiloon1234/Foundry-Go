package goway_test

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

type cursorUserTable struct {
	orm.Table[cursorUser, model.ID[cursorUser]]
	Age      orm.Number[cursorUser, int]
	Nickname orm.NullableText[cursorUser, string]
}

var cursorUsers = orm.Define("users", func(b orm.Builder[cursorUser], u *cursorUser) cursorUserTable {
	orm.TextOf(b, &u.Email)
	orm.EnumOf(b, &u.Status)
	orm.EnumOf(b, &u.Level)
	orm.NullableTimeOf(b, &u.Birthday)
	orm.NullableIDOf(b, &u.IntroducerID)
	return cursorUserTable{orm.TableOf(b, &u.ID), orm.NumberOf(b, &u.Age), orm.NullableTextOf(b, &u.Nickname)}
})
