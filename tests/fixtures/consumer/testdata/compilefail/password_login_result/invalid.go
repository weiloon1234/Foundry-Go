package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(result auth.PasswordResult[passwords.Account, model.ID[passwords.Account]]) {
	var proof auth.Proof[models.User, model.ID[models.User]] = result.Proof()
	_ = proof
}
