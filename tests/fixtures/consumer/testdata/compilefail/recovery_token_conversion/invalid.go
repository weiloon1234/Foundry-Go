package invalid

import (
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
)

func bad(token emailverification.Token[recovering.Member]) {
	_ = passwordreset.Token[recovering.Member](token)
}
