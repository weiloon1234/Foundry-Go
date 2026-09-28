package invalid

import (
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
)

func wrong(code mfa.RecoveryCode) multifactor.TOTPRequest { return multifactor.TOTPRequest{Code: code} }
