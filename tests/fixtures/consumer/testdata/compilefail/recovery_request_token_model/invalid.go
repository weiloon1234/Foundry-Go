package invalid

import (
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
)

func wrong(token passwordreset.Token[multifactor.Account]) { _ = recovering.ResetRequest{Token: token} }
