package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

func invalid(scopes auth.AccessScopes[models.Group]) {
	_ = http.AllOf[models.User](http.Scopes(scopes))
}
