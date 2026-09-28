package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong foundryhttp.Response[token.Issued[models.Group, model.ID[models.Group]]] = foundryhttp.TokenResponse[models.User, model.ID[models.User]](200, clock.System{})
