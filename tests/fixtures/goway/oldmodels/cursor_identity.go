// Package oldmodels retains the model type for the historical cursor identity fixture.
// Cursor fingerprints include the Go package path and model type name. Keeping
// oldmodels.User proves that replacing generated helpers with ORM declarations
// preserves existing cursor tokens when a consumer keeps its model identity.
// This package must contain no generated code or legacy query declarations.
package oldmodels

import (
	"foundry.test/goway/models"
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
