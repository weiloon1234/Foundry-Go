package invalid

import (
	"foundry.test/consumer/passwords"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(provider recovering.Provider, other auth.Revocation[passwords.Account, model.ID[passwords.Account]]) {
	auth.NewRevocations(provider, other)
}
