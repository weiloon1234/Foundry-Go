package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(proof auth.Proof[models.User, model.ID[models.User]]) {
	var wrong auth.Proof[models.Group, model.ID[models.User]] = proof
	_ = wrong
}
