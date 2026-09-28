package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(user models.User, scopes auth.AccessScopes[models.Group]) {
	_, _ = auth.NewScopedProof[models.User, model.ID[models.User]](user.FoundryReference(), auth.Authenticated, scopes)
}
