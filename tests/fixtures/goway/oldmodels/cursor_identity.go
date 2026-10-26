// Package oldmodels retains one handwritten historical identity fixture.
// Cursor fingerprints include the Go package path and model type name. Keeping
// oldmodels.User proves that replacing generated helpers with ORM declarations
// preserves existing cursor tokens when a consumer keeps its model identity.
// This package must contain no generated code or legacy query declarations.
package oldmodels

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// User keeps the original stored fields and unloaded relation slots. Enum codecs
// come from the current model package; only User's identity must stay historical.
type User struct {
	ID           model.ID[User]
	Email        string `foundry:"column=email_address"`
	Age          int
	Nickname     value.Nullable[string]
	Status       models.Status
	Level        models.Level
	Birthday     value.Nullable[temporal.Date]
	IntroducerID value.Nullable[model.ID[User]]
	Introducer   relation.One[User]
	Orders       relation.Many[models.Order]
}

type UserTable struct {
	orm.Table[User, model.ID[User]]
	Age      orm.Number[User, int]
	Nickname orm.NullableText[User, string]
}

var Users = orm.Define("users", func(b orm.Builder[User], u *User) UserTable {
	orm.TextOf(b, &u.Email)
	orm.EnumOf(b, &u.Status)
	orm.EnumOf(b, &u.Level)
	orm.NullableTimeOf(b, &u.Birthday)
	orm.NullableIDOf(b, &u.IntroducerID)
	return UserTable{orm.TableOf(b, &u.ID), orm.NumberOf(b, &u.Age), orm.NullableTextOf(b, &u.Nickname)}
})
