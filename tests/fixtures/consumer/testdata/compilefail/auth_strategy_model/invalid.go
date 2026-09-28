package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(provider auth.Provider[models.User, model.ID[models.User]], strategy auth.Strategy[models.Group, model.ID[models.User]]) {
	_ = auth.DefineGuard("users", provider, strategy)
}
