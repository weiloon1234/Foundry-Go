package invalid

import (
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
)

func wrong(token emailverification.Token[recovering.Member]) {
	_ = recovering.ResetRequest{Token: token}
}
