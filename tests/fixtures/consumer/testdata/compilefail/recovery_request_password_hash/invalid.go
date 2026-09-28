package invalid

import (
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/password"
)

func wrong(hash password.Hash) { _ = recovering.ResetRequest{Password: hash} }
