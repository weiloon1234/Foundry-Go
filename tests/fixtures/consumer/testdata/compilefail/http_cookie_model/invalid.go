package invalid

import (
	"context"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type DifferentModel struct{}

var cookie = foundryhttp.DefineCookie("user", foundryhttp.ModelIDCookie[models.User](), foundryhttp.DefaultCookieOptions()).Signed(foundryhttp.CookieSigner{})
var wrong model.ID[DifferentModel]
var _ = cookie.Set(context.Background(), nil, wrong)
