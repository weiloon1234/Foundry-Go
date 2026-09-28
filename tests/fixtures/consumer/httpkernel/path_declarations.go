package httpkernel

import (
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// UserPath declares a model-specific route key without handwritten codec wiring.
//
//foundry:path pattern=/users/{user}
type UserPath struct {
	User model.ID[models.User]
}

// UserFeedPath reuses the imported enum's generated membership validation.
//
//foundry:path pattern=/users/status/{state}/{page}
type UserFeedPath struct {
	Status models.Status `path:"state"`
	Page   uint16
}

//foundry:path pattern=/assets/{file...}
type AssetPath struct {
	File string
}

// ShowUser is intentionally handwritten and references a generated descriptor.
// A fresh checkout must generate successfully before this symbol can compile.
var ShowUser = foundryhttp.DefineRoute(
	foundryhttp.RouteSpec{ID: "users.show", Method: foundryhttp.GET, Access: foundryhttp.Public},
	UserPathDescriptor(),
).Within(foundryhttp.DefineScope("/api", "api"))
