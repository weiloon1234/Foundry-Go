package invalid

import (
	"foundry.test/consumer/passwords"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(proof auth.Proof[recovering.Member, model.ID[recovering.Member]], other auth.AccessScopes[passwords.Account]) {
	proof.WithAccessScopes(other)
}
